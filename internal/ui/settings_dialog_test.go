package ui

import (
	"strconv"
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
		btSettingsDialogValues{
			listenPortText:            "6881",
			metadataTimeoutText:       "45",
			connectionsLimitText:      "750",
			downloadRateLimitKiBText:  "2048",
			uploadRateLimitKiBText:    "512",
			seedRatioLimitPercentText: "150",
			seedTimeLimitMinutesText:  "90",
			trackersText:              "  udp://HOST:80/announce\nhttps://HOST/announce  ",
			sequentialDownload:        true,
			enableDHT:                 true,
			enableLSD:                 false,
			enableUPnP:                true,
			enableNATPMP:              false,
			saveMagnetTorrentFile:     true,
		},
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
	if got.BTListenPort != 6881 || got.BTMetadataTimeoutSec != 45 || got.BTConnectionsLimit != 750 {
		t.Fatalf("unexpected bt connection settings: %#v", got)
	}
	if got.BTDownloadRateLimitKiB != 2048 || got.BTUploadRateLimitKiB != 512 || got.BTSeedRatioLimitPercent != 150 || got.BTSeedTimeLimitMinutes != 90 {
		t.Fatalf("unexpected bt limit settings: %#v", got)
	}
	if !got.BTSequentialDownload || !got.BTEnableDHT || got.BTEnableLSD || !got.BTEnableUPnP || got.BTEnableNATPMP || !got.BTSaveMagnetTorrentFile {
		t.Fatalf("unexpected bt boolean settings: %#v", got)
	}
	if got.BTTrackersText != "udp://HOST:80/announce\nhttps://HOST/announce" {
		t.Fatalf("unexpected bt trackers: %q", got.BTTrackersText)
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

func TestSettingsDialogLayoutUsesScreenSizedTabs(t *testing.T) {
	if settingsDialogTabCount != 4 {
		t.Fatalf("settings tab count = %d, want 4", settingsDialogTabCount)
	}
	if settingsDialogHeight > 720 || settingsDialogMinHeight > settingsDialogHeight {
		t.Fatalf("settings dialog heights are not screen-sized: default=%d min=%d", settingsDialogHeight, settingsDialogMinHeight)
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

func TestSettingsFromDialogRejectsInvalidBTValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*btSettingsDialogValues)
	}{
		{"listen port below range", func(v *btSettingsDialogValues) { v.listenPortText = "-1" }},
		{"listen port above range", func(v *btSettingsDialogValues) { v.listenPortText = "65536" }},
		{"metadata timeout below range", func(v *btSettingsDialogValues) { v.metadataTimeoutText = "4" }},
		{"metadata timeout above range", func(v *btSettingsDialogValues) { v.metadataTimeoutText = "301" }},
		{"connections below range", func(v *btSettingsDialogValues) { v.connectionsLimitText = "19" }},
		{"connections above range", func(v *btSettingsDialogValues) { v.connectionsLimitText = "2001" }},
		{"negative download rate", func(v *btSettingsDialogValues) { v.downloadRateLimitKiBText = "-1" }},
		{"negative upload rate", func(v *btSettingsDialogValues) { v.uploadRateLimitKiBText = "-1" }},
		{"seed ratio below range", func(v *btSettingsDialogValues) { v.seedRatioLimitPercentText = "-1" }},
		{"seed ratio above range", func(v *btSettingsDialogValues) { v.seedRatioLimitPercentText = "10001" }},
		{"seed time below range", func(v *btSettingsDialogValues) { v.seedTimeLimitMinutesText = "-1" }},
		{"seed time above range", func(v *btSettingsDialogValues) { v.seedTimeLimitMinutesText = "43201" }},
		{"non numeric value", func(v *btSettingsDialogValues) { v.listenPortText = "port" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validBTDialogValues()
			test.mutate(&values)
			if _, err := validSettingsFromDialogWithBT(config.Settings{}, "", "mp4", values); err == nil {
				t.Fatal("expected invalid bt settings error")
			}
		})
	}
}

func TestSettingsFromDialogAcceptsBTBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		values btSettingsDialogValues
	}{
		{
			name: "minimum",
			values: btSettingsDialogValues{
				listenPortText:            "0",
				metadataTimeoutText:       "5",
				connectionsLimitText:      "20",
				downloadRateLimitKiBText:  "0",
				uploadRateLimitKiBText:    "0",
				seedRatioLimitPercentText: "0",
				seedTimeLimitMinutesText:  "0",
			},
		},
		{
			name: "maximum",
			values: btSettingsDialogValues{
				listenPortText:            "65535",
				metadataTimeoutText:       "300",
				connectionsLimitText:      "2000",
				downloadRateLimitKiBText:  "1099511627776",
				uploadRateLimitKiBText:    "1099511627776",
				seedRatioLimitPercentText: "10000",
				seedTimeLimitMinutesText:  "43200",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validSettingsFromDialogWithBT(config.Settings{}, "", "mp4", test.values)
			if err != nil {
				t.Fatal(err)
			}
			if got.BTListenPort != mustAtoi(t, test.values.listenPortText) ||
				got.BTMetadataTimeoutSec != mustAtoi(t, test.values.metadataTimeoutText) ||
				got.BTConnectionsLimit != mustAtoi(t, test.values.connectionsLimitText) ||
				got.BTDownloadRateLimitKiB != mustParseInt64(t, test.values.downloadRateLimitKiBText) ||
				got.BTUploadRateLimitKiB != mustParseInt64(t, test.values.uploadRateLimitKiBText) ||
				got.BTSeedRatioLimitPercent != mustAtoi(t, test.values.seedRatioLimitPercentText) ||
				got.BTSeedTimeLimitMinutes != mustAtoi(t, test.values.seedTimeLimitMinutesText) {
				t.Fatalf("bt boundaries changed: %#v", got)
			}
		})
	}
}

func validSettingsFromDialog(current config.Settings, headersText string, outputFormat string) (config.Settings, error) {
	return validSettingsFromDialogWithBT(current, headersText, outputFormat, validBTDialogValues())
}

func validSettingsFromDialogWithBT(current config.Settings, headersText string, outputFormat string, bt btSettingsDialogValues) (config.Settings, error) {
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
		bt,
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

func validBTDialogValues() btSettingsDialogValues {
	return btSettingsDialogValues{
		listenPortText:            "0",
		metadataTimeoutText:       "30",
		connectionsLimitText:      "500",
		downloadRateLimitKiBText:  "0",
		uploadRateLimitKiBText:    "0",
		seedRatioLimitPercentText: "0",
		seedTimeLimitMinutesText:  "0",
		enableDHT:                 true,
		enableLSD:                 true,
		enableUPnP:                true,
		enableNATPMP:              true,
	}
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustParseInt64(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
