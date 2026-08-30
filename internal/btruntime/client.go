// Package btruntime is the lightweight client for the optional BitTorrent
// runtime process. Keeping this package free of the torrent engine prevents
// the main GUI executable from statically linking the entire BT stack.
package btruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ghost-downloader-go-win32/internal/core"
	appwin32 "ghost-downloader-go-win32/internal/win32"
)

const stateFiles = "files"

// Options is serialized to gd3-bt-runtime for source resolution.
type Options struct {
	DownloadDir string
	ProxyURL    string
	Headers     map[string]string

	MetadataTimeout       time.Duration
	ListenPort            int
	ConnectionsLimit      int
	DownloadRateLimit     int64
	UploadRateLimit       int64
	EnableDHT             bool
	EnableLSD             bool
	EnableUPnP            bool
	EnableNATPMP          bool
	SequentialDownload    bool
	SeedRatioLimitPercent int
	SeedTimeLimitMinutes  int
	ExtraTrackers         []string
	SaveMagnetTorrentFile bool

	// RuntimePath overrides the sibling gd3-bt-runtime.exe lookup.
	RuntimePath string `json:"-"`
}

// File is the lightweight UI representation persisted in Task.Stage.State.
type File struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Selected   bool   `json:"selected"`
	Priority   int    `json:"priority"`
	Downloaded int64  `json:"downloadedBytes,omitempty"`
	Completed  bool   `json:"completed,omitempty"`
}

// ResolveRequest and RuntimeMessage form the version-1 JSON stream protocol.
type ResolveRequest struct {
	Source  string  `json:"source"`
	Options Options `json:"options"`
}

type RuntimeMessage struct {
	Task   *core.Task           `json:"task,omitempty"`
	Update *core.ProgressUpdate `json:"update,omitempty"`
	Error  string               `json:"error,omitempty"`
	Done   bool                 `json:"done,omitempty"`
}

// Worker proxies a scheduler Worker to a separate runtime process.
type Worker struct {
	RuntimePath string
}

func IsSource(source string) bool {
	text := strings.TrimSpace(source)
	if text == "" {
		return false
	}
	parsed, err := url.Parse(text)
	if err == nil {
		switch strings.ToLower(parsed.Scheme) {
		case "magnet":
			for _, exactTopic := range parsed.Query()["xt"] {
				if strings.HasPrefix(strings.ToLower(exactTopic), "urn:btih:") && len(exactTopic) > len("urn:btih:") {
					return true
				}
			}
			return false
		case "http", "https", "file":
			return strings.EqualFold(filepath.Ext(parsed.Path), ".torrent")
		}
	}
	return !strings.Contains(text, "://") && strings.EqualFold(filepath.Ext(text), ".torrent")
}

func Resolve(ctx context.Context, source string, options Options) (core.Task, error) {
	request := ResolveRequest{Source: source, Options: options}
	var response RuntimeMessage
	if err := runSingle(ctx, runtimePath(options.RuntimePath), "resolve", request, &response); err != nil {
		return core.Task{}, err
	}
	if response.Error != "" {
		return core.Task{}, errors.New(response.Error)
	}
	if response.Task == nil {
		return core.Task{}, errors.New("BitTorrent runtime returned no task")
	}
	return *response.Task, nil
}

func (w Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	path := runtimePath(w.RuntimePath)
	command := exec.Command(path, "run")
	appwin32.HideCommandWindow(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("open BitTorrent runtime input: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open BitTorrent runtime output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return runtimeStartError(path, err)
	}
	if err := json.NewEncoder(stdin).Encode(task); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("send task to BitTorrent runtime: %w", err)
	}

	var closeOnce sync.Once
	closeInput := func() { closeOnce.Do(func() { _ = stdin.Close() }) }
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			closeInput()
		case <-done:
		}
	}()

	decoder := json.NewDecoder(stdout)
	var runtimeErr error
	sawDone := false
	for {
		var message RuntimeMessage
		if err := decoder.Decode(&message); err != nil {
			if !errors.Is(err, io.EOF) {
				runtimeErr = fmt.Errorf("decode BitTorrent runtime output: %w", err)
			}
			break
		}
		if message.Update != nil && report != nil {
			report(*message.Update)
		}
		if message.Error != "" {
			runtimeErr = errors.New(message.Error)
		}
		if message.Done {
			sawDone = true
			break
		}
	}
	close(done)
	closeInput()
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runtimeErr != nil {
		return runtimeErr
	}
	if !sawDone {
		return errors.New("BitTorrent runtime closed without a terminal message")
	}
	if waitErr != nil {
		return fmt.Errorf("BitTorrent runtime exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (w Worker) ResetTask(task core.Task) (core.Task, error) {
	var response RuntimeMessage
	if err := runSingle(context.Background(), runtimePath(w.RuntimePath), "reset", task, &response); err != nil {
		return core.Task{}, err
	}
	if response.Error != "" {
		return core.Task{}, errors.New(response.Error)
	}
	if response.Task == nil {
		return core.Task{}, errors.New("BitTorrent runtime returned no reset task")
	}
	return *response.Task, nil
}

func FilesFromTask(task core.Task) ([]File, error) {
	if task.Stage.State == nil || strings.TrimSpace(task.Stage.State[stateFiles]) == "" {
		return nil, errors.New("BitTorrent task has no file metadata")
	}
	var files []File
	if err := json.Unmarshal([]byte(task.Stage.State[stateFiles]), &files); err != nil {
		return nil, fmt.Errorf("decode BitTorrent files: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("BitTorrent task has no downloadable files")
	}
	return files, nil
}

func SetSelectedFiles(task core.Task, selectedIndexes []int) (core.Task, error) {
	files, err := FilesFromTask(task)
	if err != nil {
		return core.Task{}, err
	}
	selected := make(map[int]struct{}, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected[index] = struct{}{}
	}
	if len(selected) == 0 {
		return core.Task{}, errors.New("at least one BitTorrent file must be selected")
	}
	var total int64
	var count int
	for index := range files {
		_, files[index].Selected = selected[files[index].Index]
		if files[index].Selected {
			files[index].Priority = 4
			total += files[index].Size
			count++
		} else {
			files[index].Priority = 0
			files[index].Downloaded = 0
			files[index].Completed = false
		}
	}
	if count == 0 {
		return core.Task{}, errors.New("selected BitTorrent indexes contain no downloadable file")
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return core.Task{}, fmt.Errorf("encode BitTorrent files: %w", err)
	}
	state := make(map[string]string, len(task.Stage.State)+1)
	for key, value := range task.Stage.State {
		state[key] = value
	}
	task.Stage.State = state
	task.Stage.State[stateFiles] = string(encoded)
	task.FileSize = total
	task.Stage.FileSize = total
	if task.Received > total {
		task.Received = total
	}
	if task.Stage.Received > total {
		task.Stage.Received = total
	}
	return task, nil
}

func ParseTrackers(text string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, field := range strings.Fields(text) {
		tracker := strings.TrimSpace(field)
		if tracker == "" {
			continue
		}
		parsed, err := url.Parse(tracker)
		if err != nil || parsed.Host == "" {
			continue
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "udp", "ws", "wss":
		default:
			continue
		}
		if _, exists := seen[tracker]; exists {
			continue
		}
		seen[tracker] = struct{}{}
		result = append(result, tracker)
	}
	return result
}

func runSingle(ctx context.Context, path, action string, request, response any) error {
	input, err := json.Marshal(request)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, action)
	appwin32.HideCommandWindow(command)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ctx.Err()
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return runtimeStartError(path, err)
		}
		return fmt.Errorf("BitTorrent runtime %s failed: %w: %s", action, err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(stdout.Bytes(), response); err != nil {
		return fmt.Errorf("decode BitTorrent runtime %s response: %w", action, err)
	}
	return nil
}

func runtimePath(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	executable, err := os.Executable()
	if err != nil {
		return "gd3-bt-runtime.exe"
	}
	return filepath.Join(filepath.Dir(executable), "gd3-bt-runtime.exe")
}

func runtimeStartError(path string, err error) error {
	return fmt.Errorf("start BitTorrent runtime %q: %w; keep gd3-bt-runtime.exe beside gd3win.exe", path, err)
}
