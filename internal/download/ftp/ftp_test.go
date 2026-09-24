package ftpdownload

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestParseSourceAcceptsDirectory(t *testing.T) {
	info, clean, err := parseSource("ftp://user:pass@example.test/files/")
	if err != nil {
		t.Fatal(err)
	}
	if !info.directory || info.remotePath != "/files" || clean != "ftp://example.test/files/" {
		t.Fatalf("unexpected directory source: info=%#v clean=%q", info, clean)
	}
}

func TestParseSourceRejectsMalformedURLWithoutPanic(t *testing.T) {
	for _, source := range []string{"ftp://bad host/file.bin", "ftp://%zz/file.bin", "ftp://host/file\x00.bin"} {
		if _, _, err := parseSource(source); err == nil {
			t.Fatalf("malformed source accepted: %q", source)
		}
	}
}

func TestSafeFileNameForWindowsOutput(t *testing.T) {
	for input, want := range map[string]string{
		"bad:name?.iso":  "bad_name_.iso",
		"archive. ":      "archive",
		"CON":            "_CON",
		"CON.tar.gz":     "_CON.tar.gz",
		"lpt1.txt":       "_lpt1.txt",
		"aux.backup.zip": "_aux.backup.zip",
		"":               "download",
	} {
		if got := safeFileName(input); got != want {
			t.Fatalf("safeFileName(%q)=%q want %q", input, got, want)
		}
	}
}

func TestSafeRelativePathRejectsWindowsCollisions(t *testing.T) {
	seen := make(map[string]bool)
	if got, err := safeRelativePath("folder/bad:name.txt", false, seen); err != nil || got != "folder/bad_name.txt" {
		t.Fatalf("first path=%q err=%v", got, err)
	}
	if _, err := safeRelativePath("folder/bad?name.txt", false, seen); err == nil {
		t.Fatal("case-insensitive sanitized collision accepted")
	}
	if _, err := directoryOutputPath(t.TempDir(), "../escape.txt"); err == nil {
		t.Fatal("local traversal path accepted")
	}
}

func TestDirectoryEntrySizeRejectsInt64Overflow(t *testing.T) {
	if err := validateDirectoryEntrySize(uint64(1<<63 - 1)); err != nil {
		t.Fatal(err)
	}
	for _, size := range []uint64{uint64(1 << 63), ^uint64(0)} {
		if err := validateDirectoryEntrySize(size); err == nil {
			t.Fatalf("oversized FTP listing entry %d was accepted", size)
		}
	}
}

func TestParseHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, "ftp://example.test/file.bin", t.TempDir(), "", 0); err == nil {
		t.Fatal("canceled parse succeeded")
	}
}

func TestDialTimesOutStalledImplicitTLSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	info := sourceInfo{
		host:       listener.Addr().String(),
		username:   "anonymous",
		password:   "anonymous@",
		tlsMode:    "implicit",
		timeout:    100 * time.Millisecond,
		tlsConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
		remotePath: "/stalled.bin",
	}
	started := time.Now()
	if conn, err := dial(context.Background(), info, ""); err == nil {
		_ = conn.Quit()
		t.Fatal("stalled TLS handshake succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled TLS handshake ignored connection timeout: %v", elapsed)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("fixture did not accept connection")
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

func TestParseAndWorkerUseRealImplicitFTPS(t *testing.T) {
	content := bytes.Repeat([]byte("encrypted FTPS fixture\n"), 256)
	serverTLS, clientTLS := testTLSConfigs(t)
	address, closeServer := startFTPSServer(t, content, serverTLS)
	defer closeServer()
	dir := t.TempDir()
	task, err := parse(context.Background(), "ftps://user:pass@"+address+"/secure/archive.bin", dir, "", 0, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Worker{tlsConfig: clientTLS}).Run(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(task.OutputFile())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("FTPS payload mismatch")
	}
}

func TestParseAndWorkerUseRealExplicitFTPES(t *testing.T) {
	content := bytes.Repeat([]byte("explicit TLS fixture\n"), 256)
	serverTLS, clientTLS := testTLSConfigs(t)
	address, closeServer := startFTPESServer(t, content, serverTLS)
	defer closeServer()
	task, err := parse(context.Background(), "ftpes://user:pass@"+address+"/secure/explicit.bin", t.TempDir(), "", 0, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Worker{tlsConfig: clientTLS}).Run(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(task.OutputFile())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("FTPES payload mismatch")
	}
}

func TestParseAndWorkerSupportServerWithoutSizeCommand(t *testing.T) {
	content := bytes.Repeat([]byte("unknown-size fixture\n"), 128)
	address, closeServer := startFTPServerWithSize(t, content, false)
	defer closeServer()
	task, err := Parse(context.Background(), "ftp://user:pass@"+address+"/downloads/unknown.bin", t.TempDir(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if task.FileSize != 0 {
		t.Fatalf("unknown file size=%d want 0", task.FileSize)
	}
	if err := os.WriteFile(task.OutputFile()+".part", append(append([]byte{}, content...), []byte("stale tail")...), 0o600); err != nil {
		t.Fatal(err)
	}
	var final core.ProgressUpdate
	if err := (Worker{}).Run(context.Background(), task, func(update core.ProgressUpdate) { final = update }); err != nil {
		t.Fatal(err)
	}
	if final.FileSize != int64(len(content)) || final.Received != int64(len(content)) || final.Progress != 100 {
		t.Fatalf("unknown-size completion not finalized: %+v", final)
	}
	got, err := os.ReadFile(task.OutputFile())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("unknown-size FTP payload mismatch")
	}
}

func TestParseAndWorkerDownloadDirectoryRecursively(t *testing.T) {
	files := map[string][]byte{
		"/tree/root.txt":       []byte("already complete"),
		"/tree/bad:name?.txt":  []byte("safe local name"),
		"/tree/sub/nested.bin": bytes.Repeat([]byte("nested"), 128),
	}
	address, requests, stop := startFTPDirectoryServer(t, files)
	defer stop()
	downloadDir := t.TempDir()
	task, err := Parse(context.Background(), "ftp://user:pass@"+address+"/tree", downloadDir, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	wantSize := int64(len(files["/tree/root.txt"]) + len(files["/tree/bad:name?.txt"]) + len(files["/tree/sub/nested.bin"]))
	if task.Title != "tree" || task.FileSize != wantSize || task.Stage.State[stateDirectory] != "true" {
		t.Fatalf("unexpected directory task: %#v", task)
	}
	root := task.OutputFile()
	if err := prepareDirectoryRoot(root, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("wrong same-size!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "nested.bin.part"), files["/tree/sub/nested.bin"][:12], 0o600); err != nil {
		t.Fatal(err)
	}
	var final core.ProgressUpdate
	if err := (Worker{}).Run(context.Background(), task, func(update core.ProgressUpdate) { final = update }); err != nil {
		t.Fatal(err)
	}
	for remote, relative := range map[string]string{
		"/tree/root.txt":       "root.txt",
		"/tree/bad:name?.txt":  "bad_name_.txt",
		"/tree/sub/nested.bin": "sub/nested.bin",
	} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !bytes.Equal(got, files[remote]) {
			t.Fatalf("directory payload %s mismatch: %v", relative, err)
		}
	}
	if stat, err := os.Stat(filepath.Join(root, "empty")); err != nil || !stat.IsDir() {
		t.Fatalf("empty directory not preserved: %v", err)
	}
	if final.Progress != 100 || final.Received != wantSize || final.FileSize != wantSize {
		t.Fatalf("directory completion mismatch: %+v", final)
	}
	requests.mu.Lock()
	if !reflect.DeepEqual(requests.offsets["/tree/root.txt"], []int{0}) {
		t.Fatalf("unverified same-size file was not replaced: %v", requests.offsets)
	}
	if !reflect.DeepEqual(requests.offsets["/tree/sub/nested.bin"], []int{12}) {
		t.Fatalf("nested resume offsets=%v", requests.offsets["/tree/sub/nested.bin"])
	}
	firstRequestCount := len(requests.offsets["/tree/root.txt"])
	requests.mu.Unlock()
	if err := (Worker{}).Run(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	requests.mu.Lock()
	defer requests.mu.Unlock()
	if len(requests.offsets["/tree/root.txt"]) != firstRequestCount {
		t.Fatalf("verified completed file downloaded again: %v", requests.offsets)
	}
}

func TestDirectoryWorkerPreservesUnownedOutput(t *testing.T) {
	files := map[string][]byte{"/tree/data.bin": []byte("remote")}
	address, _, stop := startFTPDirectoryServer(t, files)
	defer stop()
	task, err := Parse(context.Background(), "ftp://"+address+"/tree", t.TempDir(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(task.OutputFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(task.OutputFile(), "data.bin")
	if err := os.WriteFile(keep, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Worker{}).Run(context.Background(), task, nil); err == nil {
		t.Fatal("unowned output was accepted")
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "local" {
		t.Fatalf("unowned file changed: %q, %v", got, err)
	}
}

func TestLegacyDirectoryTaskDownloadsIntoOwnedOutput(t *testing.T) {
	files := map[string][]byte{"/tree/data.bin": []byte("remote")}
	address, _, stop := startFTPDirectoryServer(t, files)
	defer stop()
	task, err := Parse(context.Background(), "ftp://"+address+"/tree", t.TempDir(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	legacyRoot := task.OutputFile()
	if err := os.Mkdir(legacyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(legacyRoot, "keep.bin")
	if err := os.WriteFile(keep, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	task, err = task.PrepareFTPDirectoryOutput()
	if err != nil {
		t.Fatal(err)
	}
	if err := (Worker{}).Run(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(task.OutputFile(), "data.bin")); err != nil || string(got) != "remote" {
		t.Fatalf("relocated payload = %q, %v", got, err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "legacy" {
		t.Fatalf("legacy output changed: %q, %v", got, err)
	}
	if err := task.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("legacy output was removed by cleanup: %v", err)
	}
}

func TestDirectoryWorkerRejectsEscapingSymlink(t *testing.T) {
	files := map[string][]byte{"/tree/sub/data.bin": []byte("remote")}
	address, _, stop := startFTPDirectoryServer(t, files)
	defer stop()
	task, err := Parse(context.Background(), "ftp://"+address+"/tree", t.TempDir(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareDirectoryRoot(task.OutputFile(), task.ID); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(task.OutputFile(), "sub")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if err := (Worker{}).Run(context.Background(), task, nil); err == nil {
		t.Fatal("escaping symlink was accepted")
	}
	if _, err := os.Stat(filepath.Join(external, "data.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file escaped into external directory: %v", err)
	}
}

func TestParseAndWorkerDownloadDirectoryWithUnknownSizes(t *testing.T) {
	files := map[string][]byte{
		"/tree/unknown.bin": bytes.Repeat([]byte("unknown directory size"), 32),
	}
	address, _, stop := startFTPDirectoryServer(t, files, true)
	defer stop()
	task, err := Parse(context.Background(), "ftp://user:pass@"+address+"/tree", t.TempDir(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if task.FileSize != 0 {
		t.Fatalf("unknown directory total=%d want 0", task.FileSize)
	}
	var entries []directoryEntry
	if err := json.Unmarshal([]byte(task.Stage.State[stateEntries]), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[2].RemotePath != "/tree/unknown.bin" || entries[2].Size != -1 {
		t.Fatalf("unexpected unknown-size manifest: %#v", entries)
	}
	if err := (Worker{}).Run(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(task.OutputFile(), "unknown.bin"))
	if err != nil || !bytes.Equal(got, files["/tree/unknown.bin"]) {
		t.Fatalf("unknown-size directory payload mismatch: %v", err)
	}
	var final core.ProgressUpdate
	if err := (Worker{}).Run(context.Background(), task, func(update core.ProgressUpdate) { final = update }); err != nil {
		t.Fatal(err)
	}
	if final.Received != int64(len(files["/tree/unknown.bin"])) || final.Progress != 100 {
		t.Fatalf("unknown-size completed-file progress mismatch: %+v", final)
	}
}

type ftpDirectoryRequests struct {
	mu      sync.Mutex
	offsets map[string][]int
}

func startFTPDirectoryServer(t *testing.T, files map[string][]byte, unknownSizes ...bool) (string, *ftpDirectoryRequests, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requests := &ftpDirectoryRequests{offsets: make(map[string][]int)}
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
				handleFTPDirectoryConnection(conn, files, requests, len(unknownSizes) > 0 && unknownSizes[0])
			}()
		}
	}()
	return listener.Addr().String(), requests, func() {
		_ = listener.Close()
		<-stopped
		wg.Wait()
	}
}

func handleFTPDirectoryConnection(conn net.Conn, files map[string][]byte, requests *ftpDirectoryRequests, unknownSizes bool) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(format string, args ...any) {
		_, _ = fmt.Fprintf(writer, format+"\r\n", args...)
		_ = writer.Flush()
	}
	reply("220 directory fixture")
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
			reply(" MLST type*;size*;")
			reply(" REST STREAM")
			reply(" EPSV")
			reply("211 End")
		case "SYST":
			reply("215 UNIX Type: L8")
		case "OPTS", "TYPE", "NOOP":
			reply("200 OK")
		case "SIZE":
			if unknownSizes {
				reply("502 unsupported")
			} else if content, ok := files[argument]; ok {
				reply("213 %d", len(content))
			} else {
				reply("550 not a file")
			}
		case "EPSV":
			if passive != nil {
				_ = passive.Close()
			}
			passive, _ = net.Listen("tcp", "127.0.0.1:0")
			reply("229 Entering Extended Passive Mode (|||%d|)", passive.Addr().(*net.TCPAddr).Port)
		case "MLSD":
			reply("150 opening data connection")
			dataConn, err := passive.Accept()
			if err != nil {
				return
			}
			for _, listing := range directoryListing(argument, files, unknownSizes) {
				_, _ = fmt.Fprintln(dataConn, listing)
			}
			_ = dataConn.Close()
			_ = passive.Close()
			passive = nil
			reply("226 listing complete")
		case "REST":
			offset, _ = strconv.Atoi(argument)
			reply("350 restart accepted")
		case "RETR":
			content, ok := files[argument]
			if !ok {
				reply("550 not found")
				continue
			}
			reply("150 opening data connection")
			dataConn, err := passive.Accept()
			if err != nil {
				return
			}
			requests.mu.Lock()
			requests.offsets[argument] = append(requests.offsets[argument], offset)
			requests.mu.Unlock()
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

func directoryListing(directory string, files map[string][]byte, unknownSizes bool) []string {
	directory = path.Clean(directory)
	directories := map[string]bool{"/tree": true, "/tree/sub": true, "/tree/empty": true}
	var lines []string
	for pathName := range directories {
		if path.Dir(pathName) == directory {
			lines = append(lines, "type=dir; "+path.Base(pathName))
		}
	}
	for pathName, content := range files {
		if path.Dir(pathName) == directory {
			if unknownSizes {
				lines = append(lines, "type=file; "+path.Base(pathName))
			} else {
				lines = append(lines, fmt.Sprintf("type=file;size=%d; %s", len(content), path.Base(pathName)))
			}
		}
	}
	sort.Strings(lines)
	return lines
}

func startFTPServer(t *testing.T, content []byte) (string, func()) {
	return startFTPServerWithSize(t, content, true)
}

func startFTPServerWithSize(t *testing.T, content []byte, supportsSize bool) (string, func()) {
	return startFTPServerWithReply(t, content, supportsSize, 226)
}

func TestWorkerRejectsFailedFinalReply(t *testing.T) {
	for _, knownSize := range []bool{true, false} {
		t.Run(fmt.Sprintf("knownSize=%v", knownSize), func(t *testing.T) {
			address, stop := startFTPServerWithReply(t, []byte("partial payload"), knownSize, 426)
			defer stop()
			task, err := Parse(context.Background(), "ftp://"+address+"/failed.bin", t.TempDir(), "", 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := (Worker{}).Run(context.Background(), task, nil); err == nil {
				t.Fatal("failed final FTP reply was reported as successful download")
			}
			if _, err := os.Stat(task.OutputFile()); !os.IsNotExist(err) {
				t.Fatalf("failed transfer published final file: %v", err)
			}
			if _, err := os.Stat(task.OutputFile() + ".part"); err != nil {
				t.Fatalf("partial file not retained: %v", err)
			}
		})
	}
}

func TestWorkerCancelWhileWaitingForFinalReply(t *testing.T) {
	for _, scheme := range []string{"ftp", "ftps", "ftpes"} {
		t.Run(scheme, func(t *testing.T) {
			payload := []byte("complete data without final control reply")
			serverTLS, clientTLS := testTLSConfigs(t)
			var address string
			var stop func()
			switch scheme {
			case "ftps":
				address, stop = startFTPSServer(t, payload, serverTLS, 0)
			case "ftpes":
				address, stop = startFTPESServer(t, payload, serverTLS, 0)
			default:
				address, stop = startFTPServerWithReply(t, payload, true, 0)
			}
			defer stop()
			task, err := parse(context.Background(), scheme+"://"+address+"/cancel.bin", t.TempDir(), "", 0, clientTLS)
			if err != nil {
				t.Fatal(err)
			}
			task.Stage.State[stateTimeout] = "3"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- (Worker{tlsConfig: clientTLS}).Run(ctx, task, nil) }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				stat, err := os.Stat(task.OutputFile() + ".part")
				if err == nil && stat.Size() == int64(len(payload)) {
					break
				}
				if time.Now().After(deadline) {
					cancel()
					<-done
					t.Fatal("data transfer did not finish")
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation result: %v", err)
				}
			case <-time.After(time.Second):
				<-done
				t.Fatal("cancellation waited for the FTP final-reply timeout")
			}
			if _, err := os.Stat(task.OutputFile()); !os.IsNotExist(err) {
				t.Fatalf("canceled transfer published final file: %v", err)
			}
			if data, err := os.ReadFile(task.OutputFile() + ".part"); err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("partial payload was not preserved: %v", err)
			}
		})
	}
}

func startFTPServerWithReply(t *testing.T, content []byte, supportsSize bool, finalCode int) (string, func()) {
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
				handleFTPConnection(conn, content, nil, nil, supportsSize, finalCode)
			}()
		}
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-stopped
		wg.Wait()
	}
}

func startFTPSServer(t *testing.T, content []byte, config *tls.Config, finalCode ...int) (string, func()) {
	t.Helper()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
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
				handleFTPConnection(conn, content, config, nil, true, finalCode...)
			}()
		}
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-stopped
		wg.Wait()
	}
}

func startFTPESServer(t *testing.T, content []byte, config *tls.Config, finalCode ...int) (string, func()) {
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
				handleFTPConnection(conn, content, nil, config, true, finalCode...)
			}()
		}
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-stopped
		wg.Wait()
	}
}

func testTLSConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "local FTPS fixture"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	return &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
}

func handleFTPConnection(conn net.Conn, content []byte, dataTLS, explicitTLS *tls.Config, supportsSize bool, finalCode ...int) {
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
		case "AUTH":
			if explicitTLS == nil || !strings.EqualFold(argument, "TLS") {
				reply("502 unsupported")
				continue
			}
			reply("234 start TLS")
			secure := tls.Server(conn, explicitTLS)
			if err := secure.Handshake(); err != nil {
				return
			}
			conn = secure
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)
			dataTLS = explicitTLS
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
		case "OPTS", "TYPE", "NOOP", "PBSZ", "PROT":
			reply("200 OK")
		case "SIZE":
			if supportsSize {
				reply("213 %d", len(content))
			} else {
				reply("502 unsupported")
			}
		case "EPSV":
			if passive != nil {
				_ = passive.Close()
			}
			if dataTLS == nil {
				passive, _ = net.Listen("tcp", "127.0.0.1:0")
			} else {
				passive, _ = tls.Listen("tcp", "127.0.0.1:0", dataTLS)
			}
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
			code := 226
			if len(finalCode) > 0 {
				code = finalCode[0]
			}
			if code != 0 {
				reply("%d transfer finished", code)
			}
		case "QUIT":
			reply("221 goodbye")
			return
		default:
			reply("502 unsupported")
		}
	}
}
