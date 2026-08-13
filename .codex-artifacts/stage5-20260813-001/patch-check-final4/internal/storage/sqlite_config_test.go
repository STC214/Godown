package storage

import (
	"path/filepath"
	"testing"

	"ghost-downloader-go-win32/internal/config"
)

func TestSQLiteConfigStoreSettingsRoundTrip(t *testing.T) {
	store, err := NewSQLiteConfigStore(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	defaults := config.Settings{
		DownloadDir:   `C:\Downloads`,
		ProxyURL:      "",
		BlockNum:      8,
		CookiesText:   "",
		RetryCount:    3,
		SpeedLimitKiB: 0,
		MaxConcurrent: 3,
	}
	loaded, err := store.LoadSettings(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != defaults {
		t.Fatalf("expected defaults, got %#v", loaded)
	}

	next := config.Settings{
		DownloadDir:                `D:\Media`,
		ProxyURL:                   "http://127.0.0.1:7890",
		CookiesText:                "session=abc",
		BlockNum:                   16,
		RetryCount:                 5,
		SpeedLimitKiB:              512,
		MaxConcurrent:              5,
		BrowserExtensionEnabled:    true,
		BrowserPairToken:           "pair-token",
		BrowserBridgePort:          14370,
		FFmpegInstallDir:           `D:\Tools\FFmpeg`,
		M3U8InstallDir:             `D:\Tools\N_m3u8DL-RE`,
		M3U8OutputFormat:           "mkv",
		M3U8ThreadCount:            12,
		M3U8RetryCount:             6,
		M3U8RequestTimeoutSec:      60,
		M3U8ConcurrentDownload:     true,
		M3U8CheckSegmentsCount:     true,
		M3U8DeleteAfterDone:        false,
		M3U8SelectAllAudioSubtitle: true,
		M3U8MP4RealTimeDecryption:  true,
		M3U8SubtitleFormat:         "VTT",
	}
	if err := store.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadSettings(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != next {
		t.Fatalf("settings mismatch: %#v", loaded)
	}
}
