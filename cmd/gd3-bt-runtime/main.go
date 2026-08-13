package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"ghost-downloader-go-win32/internal/btruntime"
	"ghost-downloader-go-win32/internal/core"
	btdownload "ghost-downloader-go-win32/internal/download/bt"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: gd3-bt-runtime.exe <resolve|run|reset>")
	}
	switch arguments[0] {
	case "resolve":
		var request btruntime.ResolveRequest
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			return err
		}
		options := request.Options
		task, err := btdownload.Resolve(context.Background(), request.Source, btdownload.Options{
			DownloadDir: options.DownloadDir, ProxyURL: options.ProxyURL, Headers: options.Headers,
			MetadataTimeout: options.MetadataTimeout, ListenPort: options.ListenPort,
			ConnectionsLimit: options.ConnectionsLimit, DownloadRateLimit: options.DownloadRateLimit,
			UploadRateLimit: options.UploadRateLimit, EnableDHT: options.EnableDHT, EnableLSD: options.EnableLSD,
			EnableUPnP: options.EnableUPnP, EnableNATPMP: options.EnableNATPMP,
			SequentialDownload: options.SequentialDownload, SeedRatioLimitPercent: options.SeedRatioLimitPercent,
			SeedTimeLimitMinutes: options.SeedTimeLimitMinutes, ExtraTrackers: options.ExtraTrackers,
			SaveMagnetTorrentFile: options.SaveMagnetTorrentFile,
		})
		return encodeResult(task, err)
	case "reset":
		var task core.Task
		if err := json.NewDecoder(os.Stdin).Decode(&task); err != nil {
			return err
		}
		reset, err := (btdownload.Worker{}).ResetTask(task)
		return encodeResult(reset, err)
	case "run":
		return runWorker()
	default:
		return fmt.Errorf("unknown action %q", arguments[0])
	}
}

func encodeResult(task core.Task, err error) error {
	message := btruntime.RuntimeMessage{Task: &task, Done: true}
	if err != nil {
		message.Task = nil
		message.Error = err.Error()
	}
	return json.NewEncoder(os.Stdout).Encode(message)
}

func runWorker() error {
	decoder := json.NewDecoder(os.Stdin)
	var task core.Task
	if err := decoder.Decode(&task); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		var discard any
		_ = decoder.Decode(&discard)
		cancel()
	}()

	encoder := json.NewEncoder(os.Stdout)
	var outputMu sync.Mutex
	emit := func(message btruntime.RuntimeMessage) {
		outputMu.Lock()
		_ = encoder.Encode(message)
		outputMu.Unlock()
	}
	err := (btdownload.Worker{}).Run(ctx, task, func(update core.ProgressUpdate) {
		emit(btruntime.RuntimeMessage{Update: &update})
	})
	message := btruntime.RuntimeMessage{Done: true}
	if err != nil && !errors.Is(err, context.Canceled) {
		message.Error = err.Error()
	}
	emit(message)
	return nil
}
