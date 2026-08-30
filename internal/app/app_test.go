package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/browserbridge"
	"ghost-downloader-go-win32/internal/config"
)

func TestSafeBrowserTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim and replace", input: `  report?.txt.  `, want: "report_.txt"},
		{name: "all reserved", input: `a<>:"/\|?*b`, want: "a_________b"},
		{name: "control", input: "line\nfeed", want: "line_feed"},
		{name: "empty after trim", input: " . . ", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := safeBrowserTitle(test.input); got != test.want {
				t.Fatalf("safeBrowserTitle(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMergeHeaders(t *testing.T) {
	base := map[string]string{"Accept": "application/json", "X-Base": "keep"}
	extra := map[string]string{"Accept": "video/mp4", "X-Blank": "  ", "X-New": "added"}
	got := mergeHeaders(base, extra)
	want := map[string]string{"Accept": "video/mp4", "X-Base": "keep", "X-New": "added"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeHeaders() = %#v, want %#v", got, want)
	}
	got["X-Base"] = "changed"
	if base["X-Base"] != "keep" {
		t.Fatal("mergeHeaders mutated the base map")
	}
}

func TestBTOptionsFromSettings(t *testing.T) {
	settings := config.Settings{
		DownloadDir:             `D:\Downloads`,
		ProxyURL:                "socks5://127.0.0.1:1080",
		BTMetadataTimeoutSec:    45,
		BTListenPort:            51413,
		BTConnectionsLimit:      250,
		BTDownloadRateLimitKiB:  128,
		BTUploadRateLimitKiB:    64,
		BTEnableDHT:             true,
		BTEnableLSD:             true,
		BTEnableUPnP:            true,
		BTEnableNATPMP:          true,
		BTSequentialDownload:    true,
		BTSeedRatioLimitPercent: 150,
		BTSeedTimeLimitMinutes:  30,
		BTTrackersText:          "udp://one.example:80/announce\nudp://two.example:80/announce",
		BTSaveMagnetTorrentFile: true,
	}
	headers := map[string]string{"Authorization": "token"}
	got := btOptionsFromSettings(settings, headers)
	if got.DownloadDir != settings.DownloadDir || got.ProxyURL != settings.ProxyURL || !reflect.DeepEqual(got.Headers, headers) {
		t.Fatalf("basic options mismatch: %#v", got)
	}
	if got.MetadataTimeout != 45*time.Second || got.ListenPort != 51413 || got.ConnectionsLimit != 250 {
		t.Fatalf("connection options mismatch: %#v", got)
	}
	if got.DownloadRateLimit != 128*1024 || got.UploadRateLimit != 64*1024 {
		t.Fatalf("rate conversion mismatch: download=%d upload=%d", got.DownloadRateLimit, got.UploadRateLimit)
	}
	if len(got.ExtraTrackers) != 2 || !got.SaveMagnetTorrentFile || !got.SequentialDownload {
		t.Fatalf("tracker or mode options mismatch: %#v", got)
	}
}

func TestMergeResourcesFromBrowser(t *testing.T) {
	input := []browserbridge.MergeResourceRequest{{
		URL: "https://example.test/video", Filename: "video.m4s", Mime: "video/mp4",
		Size: 42, Headers: map[string]string{"Referer": "https://example.test"}, SupportsRange: true,
	}}
	got := mergeResourcesFromBrowser(input)
	if len(got) != 1 || got[0].URL != input[0].URL || got[0].Filename != input[0].Filename || got[0].Mime != input[0].Mime || got[0].Size != 42 || !got[0].SupportsRange {
		t.Fatalf("unexpected resources: %#v", got)
	}
}

func TestCreateTaskFromSourceHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Test") != "yes" {
			t.Errorf("X-Test header = %q", request.Header.Get("X-Test"))
		}
		response.Header().Set("Content-Range", "bytes 1-1/10")
		response.Header().Set("Content-Disposition", `attachment; filename="sample.bin"`)
		response.WriteHeader(http.StatusPartialContent)
		_, _ = response.Write([]byte("x"))
	}))
	defer server.Close()

	settings := config.Settings{DownloadDir: t.TempDir(), BlockNum: 4, RetryCount: 2}
	task, err := createTaskFromSource(context.Background(), server.URL+"/download", settings, map[string]string{"X-Test": "yes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "sample.bin" || task.FileSize != 10 || task.Path != settings.DownloadDir {
		t.Fatalf("unexpected task: %#v", task)
	}
	if task.Stage.Kind != "http" || task.Stage.BlockNum != 4 || task.Stage.MaxRetries != 2 || !task.Stage.SupportsRange {
		t.Fatalf("unexpected stage: %#v", task.Stage)
	}
}

func TestCreateTaskFromBrowserAppliesOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Request") != "override" || request.Header.Get("X-Settings") != "base" {
			t.Errorf("unexpected headers: %#v", request.Header)
		}
		response.Header().Set("Content-Range", "bytes 1-1/20")
		response.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	settings := config.Settings{DownloadDir: t.TempDir(), HeadersText: "X-Settings: base", BlockNum: 4, RetryCount: 3}
	overridePath := t.TempDir()
	task, err := createTaskFromBrowser(context.Background(), browserbridge.CreateTaskRequest{
		URL: server.URL + "/browser.bin", Title: "browser?.bin", Path: overridePath,
		Headers: map[string]string{"X-Request": "override", "X-Blank": "  "}, PreBlockNum: 6,
	}, settings, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "browser_.bin" || task.Path != overridePath || task.Stage.BlockNum != 6 {
		t.Fatalf("browser overrides not applied: %#v", task)
	}
	if task.Stage.Headers["X-Request"] != "override" || task.Stage.Headers["X-Blank"] != "" {
		t.Fatalf("request headers not normalized: %#v", task.Stage.Headers)
	}
}

func TestCreateTaskFromBrowserResourceMerge(t *testing.T) {
	settings := config.Settings{DownloadDir: t.TempDir(), RetryCount: 2}
	resources := []browserbridge.MergeResourceRequest{
		{URL: "https://example.test/video.m4s", Filename: "video.m4s", Mime: "video/mp4", Size: 100},
		{URL: "https://example.test/audio.m4s", Filename: "audio.m4s", Mime: "audio/mp4", Size: 20},
	}
	task, err := createTaskFromBrowser(context.Background(), browserbridge.CreateTaskRequest{
		Source: "resource_merge", Title: "combined", Resources: resources,
	}, settings, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "combined.mp4" || task.FileSize != 120 || task.Stage.Kind != "ffmpeg_merge" {
		t.Fatalf("unexpected merge task: %#v", task)
	}

	_, err = createTaskFromBrowser(context.Background(), browserbridge.CreateTaskRequest{
		Source: "resource_merge", Title: "invalid", Resources: resources[:1],
	}, settings, nil, nil)
	if err == nil {
		t.Fatal("resource merge accepted a single resource")
	}
}
