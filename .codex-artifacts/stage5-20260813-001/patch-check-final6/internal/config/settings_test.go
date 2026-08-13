package config

import (
	"encoding/json"
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

func TestDefaultSettingsIncludesBTDefaults(t *testing.T) {
	paths := Paths{
		RuntimeDir:  filepath.Join("C:", "AppData", "runtimes"),
		DownloadDir: filepath.Join("C:", "Downloads"),
	}
	settings := DefaultSettings(paths)

	if settings.BTListenPort != 0 || settings.BTMetadataTimeoutSec != 30 || settings.BTConnectionsLimit != 500 {
		t.Fatalf("unexpected bt connection defaults: %#v", settings)
	}
	if settings.BTDownloadRateLimitKiB != 0 || settings.BTUploadRateLimitKiB != 0 {
		t.Fatalf("unexpected bt rate defaults: %#v", settings)
	}
	if settings.BTSequentialDownload || settings.BTSaveMagnetTorrentFile || settings.BTSeedRatioLimitPercent != 0 || settings.BTSeedTimeLimitMinutes != 0 {
		t.Fatalf("unexpected bt task defaults: %#v", settings)
	}
	if !settings.BTEnableDHT || !settings.BTEnableLSD || !settings.BTEnableUPnP || !settings.BTEnableNATPMP {
		t.Fatalf("expected bt discovery defaults to be enabled: %#v", settings)
	}
}

func TestSettingsNormalizedRepairsInvalidBTValuesWithoutChangingBooleans(t *testing.T) {
	paths := Paths{
		RuntimeDir:  filepath.Join("C:", "AppData", "runtimes"),
		DownloadDir: filepath.Join("C:", "Downloads"),
	}
	settings := Settings{
		BTListenPort:            65536,
		BTMetadataTimeoutSec:    4,
		BTConnectionsLimit:      2001,
		BTDownloadRateLimitKiB:  -1,
		BTUploadRateLimitKiB:    -2,
		BTEnableDHT:             false,
		BTEnableLSD:             false,
		BTEnableUPnP:            false,
		BTEnableNATPMP:          false,
		BTSeedRatioLimitPercent: 10001,
		BTSeedTimeLimitMinutes:  43201,
	}.Normalized(paths)

	if settings.BTListenPort != 0 || settings.BTMetadataTimeoutSec != 30 || settings.BTConnectionsLimit != 500 {
		t.Fatalf("unexpected normalized bt connection values: %#v", settings)
	}
	if settings.BTDownloadRateLimitKiB != 0 || settings.BTUploadRateLimitKiB != 0 || settings.BTSeedRatioLimitPercent != 0 || settings.BTSeedTimeLimitMinutes != 0 {
		t.Fatalf("unexpected normalized bt limits: %#v", settings)
	}
	if settings.BTEnableDHT || settings.BTEnableLSD || settings.BTEnableUPnP || settings.BTEnableNATPMP {
		t.Fatalf("normalization changed explicit false bt settings: %#v", settings)
	}
}

func TestSettingsNormalizedAcceptsBTBoundaries(t *testing.T) {
	paths := Paths{
		RuntimeDir:  filepath.Join("C:", "AppData", "runtimes"),
		DownloadDir: filepath.Join("C:", "Downloads"),
	}
	tests := []Settings{
		{
			BTListenPort:            0,
			BTMetadataTimeoutSec:    5,
			BTConnectionsLimit:      20,
			BTDownloadRateLimitKiB:  0,
			BTUploadRateLimitKiB:    0,
			BTSeedRatioLimitPercent: 0,
			BTSeedTimeLimitMinutes:  0,
		},
		{
			BTListenPort:            65535,
			BTMetadataTimeoutSec:    300,
			BTConnectionsLimit:      2000,
			BTDownloadRateLimitKiB:  1 << 40,
			BTUploadRateLimitKiB:    1 << 40,
			BTSeedRatioLimitPercent: 10000,
			BTSeedTimeLimitMinutes:  43200,
		},
	}

	for _, input := range tests {
		got := input.Normalized(paths)
		if got.BTListenPort != input.BTListenPort ||
			got.BTMetadataTimeoutSec != input.BTMetadataTimeoutSec ||
			got.BTConnectionsLimit != input.BTConnectionsLimit ||
			got.BTDownloadRateLimitKiB != input.BTDownloadRateLimitKiB ||
			got.BTUploadRateLimitKiB != input.BTUploadRateLimitKiB ||
			got.BTSeedRatioLimitPercent != input.BTSeedRatioLimitPercent ||
			got.BTSeedTimeLimitMinutes != input.BTSeedTimeLimitMinutes {
			t.Fatalf("valid bt boundary changed: input=%#v got=%#v", input, got)
		}
	}
}

func TestOldSettingsPayloadKeepsDefaultBTDiscoveryFlags(t *testing.T) {
	paths := Paths{
		RuntimeDir:  filepath.Join("C:", "AppData", "runtimes"),
		DownloadDir: filepath.Join("C:", "Downloads"),
	}
	settings := DefaultSettings(paths)
	if err := json.Unmarshal([]byte(`{"maxConcurrent":7}`), &settings); err != nil {
		t.Fatal(err)
	}
	settings = settings.Normalized(paths)

	if settings.MaxConcurrent != 7 {
		t.Fatalf("old payload was not applied: %#v", settings)
	}
	if !settings.BTEnableDHT || !settings.BTEnableLSD || !settings.BTEnableUPnP || !settings.BTEnableNATPMP {
		t.Fatalf("missing bt fields did not retain defaults: %#v", settings)
	}
}
