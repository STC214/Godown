package ui

import (
	"testing"

	"ghost-downloader-go-win32/internal/config"
)

func TestSettingsFromDialog(t *testing.T) {
	current := config.Settings{}
	got, err := settingsFromDialog(
		current,
		`D:\Downloads`,
		"http://127.0.0.1:7890",
		"User-Agent: GD3",
		"a=b\nc=d",
		"16",
		"5",
		"4",
		"1024",
		true,
		"pair-token",
		"14370",
		`C:\Runtimes\FFmpeg`,
		`C:\Runtimes\N_m3u8DL-RE`,
		"mkv",
		"12",
		"6",
		"60",
		"vtt",
		true,
		true,
		false,
		true,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.DownloadDir != `D:\Downloads` || got.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected path/proxy settings: %#v", got)
	}
	if got.HeadersText != "User-Agent: GD3" {
		t.Fatalf("unexpected headers: %#v", got)
	}
	if got.CookiesText != "a=b; c=d" {
		t.Fatalf("unexpected cookies: %#v", got)
	}
	if got.BlockNum != 16 || got.MaxConcurrent != 5 || got.RetryCount != 4 || got.SpeedLimitKiB != 1024 {
		t.Fatalf("unexpected numeric settings: %#v", got)
	}
	if !got.BrowserExtensionEnabled || got.BrowserPairToken != "pair-token" || got.BrowserBridgePort != 14370 {
		t.Fatalf("unexpected browser settings: %#v", got)
	}
	if got.FFmpegInstallDir != `C:\Runtimes\FFmpeg` {
		t.Fatalf("unexpected ffmpeg settings: %#v", got)
	}
	if got.M3U8InstallDir != `C:\Runtimes\N_m3u8DL-RE` || got.M3U8OutputFormat != "mkv" || got.M3U8SubtitleFormat != "VTT" {
		t.Fatalf("unexpected m3u8 string settings: %#v", got)
	}
	if got.M3U8ThreadCount != 12 || got.M3U8RetryCount != 6 || got.M3U8RequestTimeoutSec != 60 {
		t.Fatalf("unexpected m3u8 numeric settings: %#v", got)
	}
	if !got.M3U8ConcurrentDownload || !got.M3U8CheckSegmentsCount || got.M3U8DeleteAfterDone || !got.M3U8SelectAllAudioSubtitle || !got.M3U8MP4RealTimeDecryption {
		t.Fatalf("unexpected m3u8 bool settings: %#v", got)
	}
}

func TestSettingsFromDialogRejectsInvalidHeaders(t *testing.T) {
	_, err := validSettingsFromDialog(config.Settings{}, "Broken", "mp4")
	if err == nil {
		t.Fatal("expected invalid headers error")
	}
}

func TestSettingsFromDialogRejectsInvalidM3U8Output(t *testing.T) {
	_, err := validSettingsFromDialog(config.Settings{}, "", "avi")
	if err == nil {
		t.Fatal("expected invalid m3u8 output error")
	}
}

func validSettingsFromDialog(current config.Settings, headersText string, outputFormat string) (config.Settings, error) {
	return settingsFromDialog(
		current,
		`D:\Downloads`,
		"",
		headersText,
		"",
		"8",
		"3",
		"3",
		"0",
		false,
		"pair-token",
		"14370",
		`C:\Runtimes\FFmpeg`,
		`C:\Runtimes\N_m3u8DL-RE`,
		outputFormat,
		"8",
		"3",
		"100",
		"SRT",
		true,
		true,
		true,
		true,
		true,
	)
}
