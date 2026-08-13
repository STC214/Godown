package m3u8download

import (
	"testing"

	"ghost-downloader-go-win32/internal/config"
)

func TestOptionsFromSettings(t *testing.T) {
	settings := config.Settings{
		DownloadDir:                `D:\Media`,
		ProxyURL:                   "http://127.0.0.1:7890",
		FFmpegInstallDir:           `C:\Tools\FFmpeg`,
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
	options := OptionsFromSettings(settings, "https://example.test/live.m3u8", "live.ts", "tsk_1", true)
	if options.SaveDir != settings.DownloadDir || options.ProxyURL != settings.ProxyURL || !options.IsLive {
		t.Fatalf("unexpected mapped options: %#v", options)
	}
	if options.OutputFormat != "mkv" || options.ThreadCount != 12 || options.SubtitleFormat != "VTT" {
		t.Fatalf("unexpected m3u8 options: %#v", options)
	}
}

func TestOptionsFromTask(t *testing.T) {
	task := testM3U8Task()
	options := OptionsFromTask(task)
	if options.URL != task.Stage.URL || options.SaveDir != task.Path || options.Title != task.Title || options.TaskID != task.ID {
		t.Fatalf("unexpected core fields: %#v", options)
	}
	if !options.IsLive || options.OutputFormat != "mkv" || options.RequestTimeoutSec != 42 || !options.SelectAllAudioSubtitle {
		t.Fatalf("unexpected state fields: %#v", options)
	}
}
