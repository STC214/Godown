package btruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"ghost-downloader-go-win32/internal/core"
)

func TestRuntimeProcessResolveAndDownload(t *testing.T) {
	runtimePath := os.Getenv("GD3_BT_RUNTIME_TEST_PATH")
	if runtimePath == "" {
		t.Skip("set GD3_BT_RUNTIME_TEST_PATH to run the packaged-runtime integration test")
	}
	payload := bytes.Repeat([]byte("split-runtime-fixture\n"), 2048)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.ServeContent(response, request, "fixture.bin", time.Unix(0, 0), bytes.NewReader(payload))
	}))
	defer server.Close()

	info := metainfo.Info{Name: "fixture.bin", PieceLength: 16 << 10, Length: int64(len(payload))}
	if err := info.GeneratePieces(func(metainfo.FileInfo) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, UrlList: []string{server.URL + "/fixture.bin"}}
	var encoded bytes.Buffer
	if err := mi.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	torrentPath := filepath.Join(t.TempDir(), "fixture.torrent")
	if err := os.WriteFile(torrentPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	task, err := Resolve(context.Background(), torrentPath, Options{
		DownloadDir: downloadDir, ConnectionsLimit: 20, RuntimePath: runtimePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(task)
	if err != nil || len(files) != 1 || files[0].Size != int64(len(payload)) {
		t.Fatalf("resolved files=%#v err=%v", files, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	completed := false
	err = (Worker{RuntimePath: runtimePath}).Run(ctx, task, func(update core.ProgressUpdate) {
		if update.FileSize == int64(len(payload)) && update.Received == update.FileSize {
			completed = true
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || !completed {
		t.Fatalf("run err=%v completed=%v", err, completed)
	}
	actual, err := os.ReadFile(filepath.Join(downloadDir, task.Title))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatal("runtime output does not match fixture")
	}
}
