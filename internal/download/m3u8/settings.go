package m3u8download

import (
	"strconv"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
)

func OptionsFromSettings(settings config.Settings, rawURL, title, taskID string, isLive bool) Options {
	return Options{
		URL:                    rawURL,
		SaveDir:                settings.DownloadDir,
		Title:                  title,
		TaskID:                 taskID,
		FFmpegPath:             FindFFmpegExecutable(settings.FFmpegInstallDir),
		Headers:                nil,
		ProxyURL:               settings.ProxyURL,
		ThreadCount:            settings.M3U8ThreadCount,
		RetryCount:             settings.M3U8RetryCount,
		RequestTimeoutSec:      settings.M3U8RequestTimeoutSec,
		ConcurrentDownload:     settings.M3U8ConcurrentDownload,
		CheckSegmentsCount:     settings.M3U8CheckSegmentsCount,
		DeleteAfterDone:        settings.M3U8DeleteAfterDone,
		OutputFormat:           settings.M3U8OutputFormat,
		SubtitleFormat:         settings.M3U8SubtitleFormat,
		SelectAllAudioSubtitle: settings.M3U8SelectAllAudioSubtitle,
		MP4RealTimeDecryption:  settings.M3U8MP4RealTimeDecryption,
		IsLive:                 isLive,
	}
}

func OptionsFromTask(task core.Task) Options {
	state := task.Stage.State
	return Options{
		URL:                    task.Stage.URL,
		SaveDir:                task.Path,
		Title:                  task.Title,
		TaskID:                 task.ID,
		Headers:                task.Stage.Headers,
		ProxyURL:               task.Stage.ProxyURL,
		ThreadCount:            task.Stage.BlockNum,
		RetryCount:             task.Stage.MaxRetries,
		RequestTimeoutSec:      intState(state, "requestTimeoutSec", 100),
		ConcurrentDownload:     boolState(state, "concurrentDownload", true),
		CheckSegmentsCount:     boolState(state, "checkSegmentsCount", true),
		DeleteAfterDone:        boolState(state, "deleteAfterDone", true),
		OutputFormat:           stringState(state, "outputFormat", "mp4"),
		SubtitleFormat:         stringState(state, "subtitleFormat", "SRT"),
		SelectAllAudioSubtitle: boolState(state, "selectAllAudioSubtitle", true),
		MP4RealTimeDecryption:  boolState(state, "mp4RealTimeDecryption", true),
		IsLive:                 boolState(state, "isLive", false),
	}
}

func installDirFromTask(task core.Task) string {
	return stringState(task.Stage.State, "installDir", "")
}

func ffmpegInstallDirFromTask(task core.Task) string {
	return stringState(task.Stage.State, "ffmpegInstallDir", "")
}

func boolState(state map[string]string, key string, fallback bool) bool {
	value, ok := state[key]
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func intState(state map[string]string, key string, fallback int) int {
	value, ok := state[key]
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func stringState(state map[string]string, key string, fallback string) string {
	value, ok := state[key]
	if !ok || value == "" {
		return fallback
	}
	return value
}
