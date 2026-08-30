package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableUpdaterPrepare(t *testing.T) {
	archive := makePortableArchive(t, "1.2.0")
	hash := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/portable.zip":
			_, _ = w.Write(archive)
		case "/portable.zip.sha256":
			_, _ = fmt.Fprintf(w, "%s  portable.zip\n", hex.EncodeToString(hash[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	release := Release{Version: "1.2.0", Assets: []Asset{
		{Name: "GhostDownloader-1.2.0-windows-x64-portable.zip", DownloadURL: server.URL + "/portable.zip", Size: int64(len(archive))},
		{Name: "GhostDownloader-1.2.0-windows-x64-portable.zip.sha256", DownloadURL: server.URL + "/portable.zip.sha256"},
	}}
	prepared, err := (PortableUpdater{}).Prepare(context.Background(), release, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Version != release.Version {
		t.Fatalf("version=%q", prepared.Version)
	}
	if _, err := os.Stat(prepared.ArchivePath); err != nil {
		t.Fatal(err)
	}
}

func TestPortableUpdaterRejectsBadChecksum(t *testing.T) {
	archive := makePortableArchive(t, "1.2.0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) == ".sha256" {
			_, _ = fmt.Fprintln(w, "0000000000000000000000000000000000000000000000000000000000000000  portable.zip")
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	assets := []Asset{
		{Name: "x-windows-x64-portable.zip", DownloadURL: server.URL + "/portable.zip"},
		{Name: "x-windows-x64-portable.zip.sha256", DownloadURL: server.URL + "/portable.zip.sha256"},
	}
	if _, err := (PortableUpdater{}).Prepare(context.Background(), Release{Version: "1.2.0", Assets: assets}, t.TempDir()); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func makePortableArchive(t *testing.T, version string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "portable.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	contents := map[string][]byte{
		"gd3win.exe":          []byte("new gui"),
		"gd3-bt-runtime.exe":  []byte("new runtime"),
		"update-portable.ps1": []byte("Write-Output update"),
	}
	manifest := struct {
		Version string `json:"version"`
		Files   []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}{Version: version}
	for name, content := range contents {
		entry, _ := writer.Create(name)
		_, _ = entry.Write(content)
		hash := sha256.Sum256(content)
		manifest.Files = append(manifest.Files, struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		}{Name: name, SHA256: hex.EncodeToString(hash[:])})
	}
	data, _ := json.Marshal(manifest)
	entry, _ := writer.Create("release-manifest.json")
	_, _ = entry.Write(data)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
