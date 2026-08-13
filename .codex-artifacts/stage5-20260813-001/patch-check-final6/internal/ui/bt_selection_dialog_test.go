package ui

import (
	"reflect"
	"testing"
	"time"

	"ghost-downloader-go-win32/internal/config"
	btdownload "ghost-downloader-go-win32/internal/download/bt"
)

func TestBTSelectionModelRejectsEmptySelection(t *testing.T) {
	model := newBTSelectionModel([]btdownload.File{
		{Index: 3, Path: "one.bin", Size: 1024, Selected: true},
	})
	model.Clear()
	if _, err := model.SelectedIndexes(); err == nil {
		t.Fatal("empty selection succeeded")
	}
}

func TestBTSelectionModelInvert(t *testing.T) {
	model := newBTSelectionModel([]btdownload.File{
		{Index: 2, Path: "one.bin", Size: 10, Selected: true},
		{Index: 5, Path: "two.bin", Size: 20, Selected: false},
		{Index: 9, Path: "three.bin", Size: 30, Selected: true},
	})
	model.Invert()
	indexes, err := model.SelectedIndexes()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{5}; !reflect.DeepEqual(indexes, want) {
		t.Fatalf("selected indexes = %v, want %v", indexes, want)
	}
}

func TestBTSelectionModelSelectedSizeAndDisplay(t *testing.T) {
	model := newBTSelectionModel([]btdownload.File{
		{Index: 0, Path: "one.bin", Size: 1024, Selected: true},
		{Index: 1, Path: "two.bin", Size: 1536, Selected: true},
		{Index: 2, Path: "three.bin", Size: 4096, Selected: false},
	})
	if got, want := model.SelectedSize(), int64(2560); got != want {
		t.Fatalf("selected size = %d, want %d", got, want)
	}
	if got, want := model.Value(1, 1), "1.5 KiB"; got != want {
		t.Fatalf("display size = %v, want %q", got, want)
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
		BTUploadRateLimitKiB:    32,
		BTEnableDHT:             true,
		BTEnableLSD:             true,
		BTEnableUPnP:            true,
		BTEnableNATPMP:          true,
		BTSequentialDownload:    true,
		BTSeedRatioLimitPercent: 150,
		BTSeedTimeLimitMinutes:  60,
		BTTrackersText:          "udp://one.test:80/announce\r\nhttps://two.test/announce udp://one.test:80/announce",
		BTSaveMagnetTorrentFile: true,
	}
	headers := map[string]string{"User-Agent": "Ghost"}
	options := btOptionsFromSettings(settings, headers)

	if options.DownloadDir != settings.DownloadDir || options.ProxyURL != settings.ProxyURL || options.MetadataTimeout != 45*time.Second {
		t.Fatalf("basic options mismatch: %#v", options)
	}
	if options.ListenPort != 51413 || options.ConnectionsLimit != 250 || options.DownloadRateLimit != 128*1024 || options.UploadRateLimit != 32*1024 {
		t.Fatalf("limit options mismatch: %#v", options)
	}
	if !options.EnableDHT || !options.EnableLSD || !options.EnableUPnP || !options.EnableNATPMP || !options.SequentialDownload || !options.SaveMagnetTorrentFile {
		t.Fatalf("boolean options mismatch: %#v", options)
	}
	if options.SeedRatioLimitPercent != 150 || options.SeedTimeLimitMinutes != 60 {
		t.Fatalf("seeding options mismatch: %#v", options)
	}
	if want := []string{"udp://one.test:80/announce", "https://two.test/announce"}; !reflect.DeepEqual(options.ExtraTrackers, want) {
		t.Fatalf("trackers = %q, want %q", options.ExtraTrackers, want)
	}
	if options.Headers["User-Agent"] != "Ghost" {
		t.Fatalf("headers mismatch: %#v", options.Headers)
	}
}
