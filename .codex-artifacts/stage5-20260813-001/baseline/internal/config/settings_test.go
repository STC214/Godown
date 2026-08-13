package config

import (
	"path/filepath"
	"testing"
)

func TestSettingsNormalizedFillsM3U8Defaults(t *testing.T) {
	paths := Paths{
		RuntimeDir:  filepath.Join("C:", "AppData", "runtimes"),
		DownloadDir: filepath.Join("C:", "Downloads"),
	}
	settings := Settings{
		M3U8OutputFormat:          "avi",
		M3U8ThreadCount:           -1,
		M3U8RequestTimeoutSec:     -1,
		M3U8SubtitleFormat:        "ASS",
		M3U8MP4RealTimeDecryption: false,
	}.Normalized(paths)

	if settings.M3U8InstallDir != filepath.Join(paths.RuntimeDir, "N_m3u8DL-RE") {
		t.Fatalf("install dir=%q", settings.M3U8InstallDir)
	}
	if settings.FFmpegInstallDir != filepath.Join(paths.RuntimeDir, "FFmpeg") {
		t.Fatalf("ffmpeg dir=%q", settings.FFmpegInstallDir)
	}
	if settings.M3U8OutputFormat != "mp4" || settings.M3U8ThreadCount != 8 || settings.M3U8RequestTimeoutSec != 100 {
		t.Fatalf("unexpected m3u8 defaults: %#v", settings)
	}
	if settings.M3U8SubtitleFormat != "SRT" {
		t.Fatalf("subtitle format=%q", settings.M3U8SubtitleFormat)
	}
	if settings.BrowserPairToken == "" || settings.BrowserBridgePort != DefaultBrowserBridgePort {
		t.Fatalf("unexpected browser defaults: %#v", settings)
	}
}

func TestNewBrowserPairToken(t *testing.T) {
	token := NewBrowserPairToken()
	if len(token) < 16 {
		t.Fatalf("token too short: %q", token)
	}
	if token == NewBrowserPairToken() {
		t.Fatalf("expected fresh token")
	}
}
