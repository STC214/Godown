package ftpdownload

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestIsSource(t *testing.T) {
	for source, want := range map[string]bool{
		"ftp://example.test/file.iso":   true,
		"ftps://example.test/file.iso":  true,
		"ftpes://example.test/file.iso": true,
		"https://example.test/file":     false,
		"ftpish://example.test/file":    false,
	} {
		if got := IsSource(source); got != want {
			t.Fatalf("IsSource(%q)=%v want %v", source, got, want)
		}
	}
}

func TestParseSourceDefaultsAndRedactsCredentials(t *testing.T) {
	info, clean, err := parseSource("ftp://user:p%40ss@example.test:2121/files/a%20b.iso")
	if err != nil {
		t.Fatal(err)
	}
	if info.host != "example.test:2121" || info.remotePath != "/files/a b.iso" || info.username != "user" || info.password != "p@ss" {
		t.Fatalf("unexpected source: %#v", info)
	}
	if clean != "ftp://example.test:2121/files/a%20b.iso" {
		t.Fatalf("credentials were not redacted: %q", clean)
	}
}

func TestParseSourceFTPSUsesImplicitTLS(t *testing.T) {
	info, _, err := parseSource("ftps://example.test/secure.bin")
	if err != nil {
		t.Fatal(err)
	}
	if info.host != "example.test:990" || info.tlsMode != "implicit" {
		t.Fatalf("unexpected FTPS source: %#v", info)
	}
}

func TestParseSourceFTPESUsesExplicitTLS(t *testing.T) {
	info, _, err := parseSource("ftpes://example.test/secure.bin")
	if err != nil {
		t.Fatal(err)
	}
	if info.host != "example.test:21" || info.tlsMode != "explicit" {
		t.Fatalf("unexpected FTPES source: %#v", info)
	}
}

func TestParseRejectsDirectory(t *testing.T) {
	if _, _, err := parseSource("ftp://example.test/files/"); err == nil {
		t.Fatal("directory source accepted")
	}
}

func TestParseHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, "ftp://example.test/file.bin", t.TempDir(), "", 0); err == nil {
		t.Fatal("canceled parse succeeded")
	}
}

func TestParseAndWorkerResumeFromLocalFTP(t *testing.T) {
	content := bytes.Repeat([]byte("Ghost Downloader FTP\n"), 4096)
	address, closeServer := startFTPServer(t, content)
	defer closeServer()
	dir := t.TempDir()
	task, err := Parse(context.Background(), "ftp://user:pass@"+address+"/downloads/archive.bin", dir, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if task.FileSize != int64(len(content)) || task.Title != "archive.bin" || strings.Contains(task.URL, "user") {
		t.Fatalf("unexpected task: %#v", task)
	}
	partial := task.OutputFile() + ".part"
	if err := os.WriteFile(partial, content[:8192], 0o600); err != nil {
		t.Fatal(err)
	}
	var lastReceived int64
	if err := (Worker{}).Run(context.Background(), task, func(update core.ProgressUpdate) { lastReceived = update.Received }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(task.OutputFile())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) || lastReceived != int64(len(content)) {
		t.Fatalf("FTP result mismatch: bytes=%d progress=%d", len(got), lastReceived)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func startFTPServer(t *testing.T, content []byte) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleFTPConnection(conn, content)
			}()
		}
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-stopped
		wg.Wait()
	}
}

func handleFTPConnection(conn net.Conn, content []byte) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(format string, args ...any) {
		_, _ = fmt.Fprintf(writer, format+"\r\n", args...)
		_ = writer.Flush()
	}
	reply("220 local fixture")
	var passive net.Listener
	var offset int
	defer func() {
		if passive != nil {
			_ = passive.Close()
		}
	}()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.SplitN(strings.TrimSpace(line), " ", 2)
		command := strings.ToUpper(fields[0])
		argument := ""
		if len(fields) == 2 {
			argument = fields[1]
		}
		switch command {
		case "USER":
			reply("331 password required")
		case "PASS":
			reply("230 logged in")
		case "FEAT":
			reply("211-Features")
			reply(" SIZE")
			reply(" REST STREAM")
			reply(" EPSV")
			reply("211 End")
		case "SYST":
			reply("215 UNIX Type: L8")
		case "OPTS", "TYPE", "NOOP":
			reply("200 OK")
		case "SIZE":
			reply("213 %d", len(content))
		case "EPSV":
			if passive != nil {
				_ = passive.Close()
			}
			passive, _ = net.Listen("tcp", "127.0.0.1:0")
			port := passive.Addr().(*net.TCPAddr).Port
			reply("229 Entering Extended Passive Mode (|||%d|)", port)
		case "REST":
			offset, _ = strconv.Atoi(argument)
			reply("350 restart accepted")
		case "RETR":
			reply("150 opening data connection")
			dataConn, err := passive.Accept()
			if err != nil {
				return
			}
			if offset < 0 || offset > len(content) {
				offset = 0
			}
			_, _ = dataConn.Write(content[offset:])
			_ = dataConn.Close()
			_ = passive.Close()
			passive = nil
			offset = 0
			reply("226 transfer complete")
		case "QUIT":
			reply("221 goodbye")
			return
		default:
			reply("502 unsupported")
		}
	}
}
