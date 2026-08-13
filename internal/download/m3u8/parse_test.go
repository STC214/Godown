package m3u8download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"ghost-downloader-go-win32/internal/config"
)

func TestParseRemoteHLSManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "GD3-Test" {
			t.Fatalf("missing custom header")
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Disposition", `attachment; filename="episode.m3u8"`)
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4,\nhttps://cdn.example.test/seg.ts\n#EXT-X-ENDLIST\n"))
	}))
	defer server.Close()

	result, err := Parse(context.Background(), server.URL+"/master.m3u8", testSettings(), map[string]string{"User-Agent": "GD3-Test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.PackID != "m3u8" || result.Task.Title != "episode.mp4" {
		t.Fatalf("unexpected task: %#v", result.Task)
	}
	if result.ManifestType != ManifestHLS || result.IsLive {
		t.Fatalf("unexpected manifest metadata: %#v", result)
	}
	if result.Task.Stage.State["manifestType"] != "m3u8" || result.Task.Stage.State["isLive"] != "false" {
		t.Fatalf("unexpected stage state: %#v", result.Task.Stage.State)
	}
	if result.Task.Stage.State["ffmpegInstallDir"] != testSettings().FFmpegInstallDir {
		t.Fatalf("missing ffmpeg state: %#v", result.Task.Stage.State)
	}
}

func TestParseRemoteLiveDASHManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dash+xml")
		_, _ = w.Write([]byte(`<MPD type="dynamic"></MPD>`))
	}))
	defer server.Close()

	settings := testSettings()
	settings.M3U8OutputFormat = "mkv"
	result, err := Parse(context.Background(), server.URL+"/manifest.mpd", settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestType != ManifestDASH || !result.IsLive {
		t.Fatalf("unexpected manifest metadata: %#v", result)
	}
	if result.Task.Title != "manifest.ts" {
		t.Fatalf("live title=%q", result.Task.Title)
	}
}

func TestParseRejectsLocalRelativeManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.m3u8")
	if err := os.WriteFile(path, []byte("#EXTM3U\n#EXTINF:4,\nseg.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Parse(context.Background(), path, testSettings(), nil)
	if err == nil {
		t.Fatal("expected relative local manifest error")
	}
}

func testSettings() config.Settings {
	return config.Settings{
		DownloadDir:                `D:\Downloads`,
		ProxyURL:                   "",
		FFmpegInstallDir:           `C:\Tools\FFmpeg`,
		M3U8OutputFormat:           "mp4",
		M3U8ThreadCount:            8,
		M3U8RetryCount:             3,
		M3U8RequestTimeoutSec:      100,
		M3U8ConcurrentDownload:     true,
		M3U8CheckSegmentsCount:     true,
		M3U8DeleteAfterDone:        true,
		M3U8SelectAllAudioSubtitle: true,
		M3U8MP4RealTimeDecryption:  true,
		M3U8SubtitleFormat:         "SRT",
	}
}
