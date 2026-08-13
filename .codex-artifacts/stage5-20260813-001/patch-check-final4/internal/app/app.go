package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"ghost-downloader-go-win32/internal/browserbridge"
	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
	btdownload "ghost-downloader-go-win32/internal/download/bt"
	ffmpegdownload "ghost-downloader-go-win32/internal/download/ffmpeg"
	httpdownload "ghost-downloader-go-win32/internal/download/http"
	m3u8download "ghost-downloader-go-win32/internal/download/m3u8"
	"ghost-downloader-go-win32/internal/logging"
	"ghost-downloader-go-win32/internal/ratelimit"
	"ghost-downloader-go-win32/internal/storage"
	"ghost-downloader-go-win32/internal/ui"
	"ghost-downloader-go-win32/internal/win32"
)

var unsafeBrowserTitle = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func Run() error {
	if err := win32.EnablePerMonitorDPIAwareness(); err != nil {
		// DPI setup is best effort: old Windows builds may not expose the newer API.
		slog.Warn("failed to enable per-monitor dpi awareness", "error", err)
	}

	instance, err := win32.AcquireSingleInstance()
	if err != nil {
		if errors.Is(err, win32.ErrAlreadyRunning) {
			return err
		}
		return fmt.Errorf("acquire single-instance guard: %w", err)
	}
	defer instance.Release()

	paths, err := config.ResolvePaths()
	if err != nil {
		return fmt.Errorf("resolve app paths: %w", err)
	}

	cleanupLogger, err := logging.Initialize(paths.LogFile)
	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	defer cleanupLogger()

	slog.Info("Ghost Downloader Go + Win32 starting", "dataDir", paths.DataDir)

	configStore, err := storage.NewSQLiteConfigStore(paths.ConfigDB)
	if err != nil {
		return fmt.Errorf("initialize config store: %w", err)
	}
	defer configStore.Close()

	settings, err := configStore.LoadSettings(config.DefaultSettings(paths))
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	loadedSettings := settings
	settings = settings.Normalized(paths)
	if settings != loadedSettings {
		if err := configStore.SaveSettings(settings); err != nil {
			return fmt.Errorf("save normalized settings: %w", err)
		}
	}

	limiter := ratelimit.New(settings.SpeedLimitKiB * 1024)
	var settingsMu sync.RWMutex
	currentSettings := settings
	registry := core.NewRegistry()
	registry.Register("http", httpdownload.Worker{Limiter: limiter})
	registry.Register("bt", btdownload.Worker{})
	registry.Register("m3u8", m3u8download.Worker{})
	registry.Register("ffmpeg", ffmpegdownload.Worker{})
	taskStore, err := storage.NewSQLiteTaskStore(paths.ConfigDB)
	if err != nil {
		return fmt.Errorf("initialize task store: %w", err)
	}
	defer taskStore.Close()

	scheduler := core.NewScheduler(registry, taskStore, settings.MaxConcurrent)
	if err := scheduler.Load(); err != nil {
		slog.Warn("load remembered tasks failed", "error", err)
	}
	defer scheduler.StopAll()

	bridge := browserbridge.New(scheduler)
	bridge.SetCreateTaskFunc(func(ctx context.Context, request browserbridge.CreateTaskRequest) (core.Task, error) {
		settingsMu.RLock()
		settingsSnapshot := currentSettings
		settingsMu.RUnlock()
		return createTaskFromBrowser(ctx, request, settingsSnapshot, scheduler)
	})
	if err := bridge.Apply(browserbridge.SettingsFromConfig(settings)); err != nil {
		slog.Warn("start browser bridge failed", "error", err)
	}
	defer bridge.Stop(nil)

	return ui.Run(ui.Options{
		Paths:         paths,
		Scheduler:     scheduler,
		Limiter:       limiter,
		Settings:      settings,
		BrowserBridge: bridge,
		SaveSettings: func(next config.Settings) error {
			next = next.Normalized(paths)
			settingsMu.RLock()
			previous := currentSettings
			settingsMu.RUnlock()
			if err := bridge.Apply(browserbridge.SettingsFromConfig(next)); err != nil {
				restoreErr := bridge.Apply(browserbridge.SettingsFromConfig(previous))
				return errors.Join(err, restoreErr)
			}
			if err := configStore.SaveSettings(next); err != nil {
				restoreErr := bridge.Apply(browserbridge.SettingsFromConfig(previous))
				return errors.Join(err, restoreErr)
			}
			settingsMu.Lock()
			currentSettings = next
			settingsMu.Unlock()
			return nil
		},
	})
}

func createTaskFromBrowser(ctx context.Context, request browserbridge.CreateTaskRequest, settings config.Settings, scheduler *core.Scheduler) (core.Task, error) {
	if request.Source == "resource_merge" {
		task, err := ffmpegdownload.NewMergeTask(request.Title, request.Path, mergeResourcesFromBrowser(request.Resources), settings)
		if err != nil {
			return core.Task{}, err
		}
		if scheduler != nil {
			task.Title = httpdownload.DeduplicateTitle(task.Title, task.Path, scheduler.Snapshot())
		}
		return task, nil
	}

	headers, err := httpdownload.ParseHeaders(settings.HeadersText)
	if err != nil {
		return core.Task{}, err
	}
	headers = httpdownload.MergeCookies(headers, settings.CookiesText)
	for name, value := range request.Headers {
		if strings.TrimSpace(value) != "" {
			headers[name] = value
		}
	}

	if strings.TrimSpace(request.Path) != "" {
		settings.DownloadDir = strings.TrimSpace(request.Path)
	}
	blockNum := settings.BlockNum
	if request.PreBlockNum > 0 {
		blockNum = request.PreBlockNum
	}

	var task core.Task
	if btdownload.IsSource(request.URL) {
		task, err = btdownload.Resolve(ctx, request.URL, btOptionsFromSettings(settings, headers))
		if err != nil {
			return core.Task{}, err
		}
	} else if m3u8download.IsManifestSource(request.URL) {
		result, err := m3u8download.Parse(ctx, request.URL, settings, headers)
		if err != nil {
			return core.Task{}, err
		}
		task = result.Task
	} else {
		task, err = httpdownload.Parse(ctx, request.URL, settings.DownloadDir, blockNum, settings.RetryCount, settings.ProxyURL, headers)
		if err != nil {
			return core.Task{}, err
		}
	}
	if title := strings.TrimSpace(request.Title); title != "" {
		if safeTitle := safeBrowserTitle(title); safeTitle != "" {
			task.Title = safeTitle
		}
	}
	if scheduler != nil {
		task.Title = httpdownload.DeduplicateTitle(task.Title, task.Path, scheduler.Snapshot())
	}
	return task, nil
}

func btOptionsFromSettings(settings config.Settings, headers map[string]string) btdownload.Options {
	return btdownload.Options{
		DownloadDir:           settings.DownloadDir,
		ProxyURL:              settings.ProxyURL,
		Headers:               headers,
		MetadataTimeout:       time.Duration(settings.BTMetadataTimeoutSec) * time.Second,
		ListenPort:            settings.BTListenPort,
		ConnectionsLimit:      settings.BTConnectionsLimit,
		DownloadRateLimit:     settings.BTDownloadRateLimitKiB * 1024,
		UploadRateLimit:       settings.BTUploadRateLimitKiB * 1024,
		EnableDHT:             settings.BTEnableDHT,
		EnableLSD:             settings.BTEnableLSD,
		EnableUPnP:            settings.BTEnableUPnP,
		EnableNATPMP:          settings.BTEnableNATPMP,
		SequentialDownload:    settings.BTSequentialDownload,
		SeedRatioLimitPercent: settings.BTSeedRatioLimitPercent,
		SeedTimeLimitMinutes:  settings.BTSeedTimeLimitMinutes,
		ExtraTrackers:         btdownload.ParseTrackers(settings.BTTrackersText),
		SaveMagnetTorrentFile: settings.BTSaveMagnetTorrentFile,
	}
}

func mergeResourcesFromBrowser(resources []browserbridge.MergeResourceRequest) []ffmpegdownload.MergeResource {
	result := make([]ffmpegdownload.MergeResource, 0, len(resources))
	for _, resource := range resources {
		result = append(result, ffmpegdownload.MergeResource{
			URL:           resource.URL,
			Filename:      resource.Filename,
			Mime:          resource.Mime,
			Size:          resource.Size,
			Headers:       resource.Headers,
			SupportsRange: resource.SupportsRange,
		})
	}
	return result
}

func safeBrowserTitle(title string) string {
	title = strings.TrimSpace(title)
	title = unsafeBrowserTitle.ReplaceAllString(title, "_")
	title = strings.Trim(title, ". ")
	if title == "" {
		return ""
	}
	return title
}
