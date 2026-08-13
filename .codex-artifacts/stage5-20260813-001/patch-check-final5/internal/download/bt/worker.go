package btdownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	torrentstorage "github.com/anacrolix/torrent/storage"
	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"

	"ghost-downloader-go-win32/internal/core"
)

const defaultWorkerTick = time.Second

// Worker downloads selected files with anacrolix/torrent. Piece completion is
// kept in a per-task Bolt database, so pausing or restarting reuses verified data.
type Worker struct {
	newTransfer transferFactory
	tick        time.Duration
}

type transferFactory func(context.Context, core.Task, []File, RuntimeOptions) (transfer, error)

type transfer interface {
	Snapshot() (transferSnapshot, error)
	Close() error
}

type transferSnapshot struct {
	FileBytes         map[int]int64
	SessionDownloaded int64
	SessionUploaded   int64
	Peers             int
	Seeds             int
}

// Run owns the client lifetime and reports both numeric progress and a durable
// file/seeding checkpoint through the scheduler.
func (w Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) (runErr error) {
	state, err := RuntimeStateFromTask(task)
	if err != nil {
		return err
	}
	options := RuntimeOptionsFromTask(task)
	if report == nil {
		report = func(core.ProgressUpdate) {}
	}

	usesSlot := true
	state.Phase = "checking"
	initialPatch, err := runtimeStatePatch(state)
	if err != nil {
		return err
	}
	report(core.ProgressUpdate{
		Received:   selectedDownloaded(state.Files),
		FileSize:   selectedSize(state.Files),
		Progress:   progressPercent(selectedDownloaded(state.Files), selectedSize(state.Files)),
		Detail:     runtimeDetail(state),
		StageState: initialPatch,
		UsesSlot:   &usesSlot,
	})

	factory := w.newTransfer
	if factory == nil {
		factory = newTorrentTransfer
	}
	active, err := factory(ctx, task, state.Files, options)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, active.Close())
	}()

	tick := w.tick
	if tick <= 0 {
		tick = defaultWorkerTick
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	started := time.Now()
	previousAt := started
	var previousDownloaded int64
	var previousUploaded int64
	var seedingStarted time.Time
	baseUploaded := state.UploadedBytes
	baseSeedingSeconds := state.SeedingSeconds
	lastComplete := false

	sample := func(now time.Time) (bool, error) {
		snapshot, err := active.Snapshot()
		if err != nil {
			return false, err
		}
		for i := range state.Files {
			downloaded := snapshot.FileBytes[state.Files[i].Index]
			if downloaded < 0 {
				downloaded = 0
			}
			if downloaded > state.Files[i].Size {
				downloaded = state.Files[i].Size
			}
			if !state.Files[i].Selected {
				downloaded = 0
			}
			state.Files[i].Downloaded = downloaded
			state.Files[i].Completed = state.Files[i].Selected && downloaded >= state.Files[i].Size
		}

		received := selectedDownloaded(state.Files)
		total := selectedSize(state.Files)
		complete := received >= total
		elapsed := now.Sub(previousAt).Seconds()
		if elapsed <= 0 {
			elapsed = now.Sub(started).Seconds()
		}
		if elapsed > 0 {
			state.DownloadRate = nonNegativeRate(snapshot.SessionDownloaded-previousDownloaded, elapsed)
			state.UploadRate = nonNegativeRate(snapshot.SessionUploaded-previousUploaded, elapsed)
		}
		previousAt = now
		previousDownloaded = snapshot.SessionDownloaded
		previousUploaded = snapshot.SessionUploaded
		state.UploadedBytes = baseUploaded + max64(snapshot.SessionUploaded, 0)
		state.PeerCount = snapshot.Peers
		state.SeedCount = snapshot.Seeds
		if total > 0 {
			state.ShareRatio = float64(state.UploadedBytes) / float64(total) * 100
		}

		if complete {
			if seedingStarted.IsZero() {
				seedingStarted = now
			}
			state.SeedingSeconds = baseSeedingSeconds + int64(now.Sub(seedingStarted)/time.Second)
			state.Phase = "seeding"
			usesSlot = false
		} else {
			state.Phase = "downloading"
			usesSlot = true
		}
		workerStatus := core.StatusRunning
		if complete {
			workerStatus = core.StatusSeeding
		}
		lastComplete = complete
		patch, err := runtimeStatePatch(state)
		if err != nil {
			return false, err
		}
		report(core.ProgressUpdate{
			Received:   received,
			FileSize:   total,
			Speed:      state.DownloadRate,
			Progress:   progressPercent(received, total),
			Detail:     runtimeDetail(state),
			Status:     workerStatus,
			StageState: patch,
			UsesSlot:   &usesSlot,
		})
		return complete && shouldStopSeeding(options, state), nil
	}

	if stop, err := sample(time.Now()); err != nil {
		return err
	} else if stop {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			_, _ = sample(time.Now())
			if lastComplete {
				state.Phase = "paused_seeding"
			} else {
				state.Phase = "paused_download"
			}
			patch, patchErr := runtimeStatePatch(state)
			if patchErr == nil {
				report(core.ProgressUpdate{
					Received:   selectedDownloaded(state.Files),
					FileSize:   selectedSize(state.Files),
					Progress:   progressPercent(selectedDownloaded(state.Files), selectedSize(state.Files)),
					Detail:     runtimeDetail(state),
					StageState: patch,
					UsesSlot:   &usesSlot,
				})
			}
			return ctx.Err()
		case now := <-ticker.C:
			stop, err := sample(now)
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
		}
	}
}

// ResetTask clears transfer-derived state for Redownload while retaining the
// metainfo, selected indexes, trackers and runtime options.
func (Worker) ResetTask(task core.Task) (core.Task, error) {
	state, err := RuntimeStateFromTask(task)
	if err != nil {
		return core.Task{}, err
	}
	for index := range state.Files {
		state.Files[index].Downloaded = 0
		state.Files[index].Completed = false
	}
	state.UploadedBytes = 0
	state.SeedingSeconds = 0
	state.Phase = ""
	state.PeerCount = 0
	state.SeedCount = 0
	state.DownloadRate = 0
	state.UploadRate = 0
	state.ShareRatio = 0
	patch, err := runtimeStatePatch(state)
	if err != nil {
		return core.Task{}, err
	}
	clonedState := make(map[string]string, len(task.Stage.State)+len(patch))
	for key, value := range task.Stage.State {
		clonedState[key] = value
	}
	for key, value := range patch {
		clonedState[key] = value
	}
	task.Stage.State = clonedState
	task.FileSize = selectedSize(state.Files)
	task.Stage.FileSize = task.FileSize
	task.UsesSlot = true
	task.Detail = ""
	return task, nil
}

func shouldStopSeeding(options RuntimeOptions, state RuntimeState) bool {
	if options.SeedRatioLimitPercent > 0 && state.ShareRatio >= float64(options.SeedRatioLimitPercent) {
		return true
	}
	return options.SeedTimeLimitMinutes > 0 && state.SeedingSeconds >= int64(options.SeedTimeLimitMinutes)*60
}

func runtimeDetail(state RuntimeState) string {
	phase := strings.ReplaceAll(state.Phase, "_", " ")
	if phase == "" {
		phase = "ready"
	}
	return fmt.Sprintf(
		"BT %s | peers %d | seeds %d | down %d B/s | up %d B/s | ratio %.2f%% | seeded %ds",
		phase,
		state.PeerCount,
		state.SeedCount,
		state.DownloadRate,
		state.UploadRate,
		state.ShareRatio,
		state.SeedingSeconds,
	)
}

func selectedSize(files []File) (total int64) {
	for _, file := range files {
		if file.Selected {
			total += file.Size
		}
	}
	return
}

func selectedDownloaded(files []File) (total int64) {
	for _, file := range files {
		if file.Selected {
			total += min64(max64(file.Downloaded, 0), file.Size)
		}
	}
	return
}

func progressPercent(received, total int64) float64 {
	if total <= 0 {
		return 100
	}
	progress := float64(received) / float64(total) * 100
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func nonNegativeRate(delta int64, seconds float64) int64 {
	if delta <= 0 || seconds <= 0 {
		return 0
	}
	return int64(float64(delta) / seconds)
}

type torrentTransfer struct {
	client       *torrent.Client
	torrent      *torrent.Torrent
	storage      torrentstorage.ClientImplCloser
	webTransport *managedWebTransport
	completion   *trackedPieceCompletion
	filesByIndex map[int]*torrent.File
	selected     map[int]struct{}
	finalPaths   map[int]string
	expectedSize map[int]int64

	sequentialCancel context.CancelFunc
	sequentialDone   chan error
	closeOnce        sync.Once
	closeErr         error
}

func newTorrentTransfer(ctx context.Context, task core.Task, files []File, options RuntimeOptions) (transfer, error) {
	mi, err := DecodeMetainfo(task)
	if err != nil {
		return nil, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("decode torrent info: %w", err)
	}
	paths, err := storagePaths(task, info, files)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(task.Path, 0o755); err != nil {
		return nil, fmt.Errorf("create BitTorrent output directory: %w", err)
	}
	checkpointDir := filepath.Join(task.Path, ".gd3_bt", task.ID)
	boltCompletion, err := torrentstorage.NewBoltPieceCompletion(checkpointDir)
	if err != nil {
		return nil, fmt.Errorf("open BitTorrent completion checkpoint: %w", err)
	}
	completion := newTrackedPieceCompletion(boltCompletion)
	storageImpl := newResumeFileStorage(task.Path, paths, completion)

	config := torrent.NewDefaultClientConfig()
	config.DataDir = task.Path
	config.DefaultStorage = storageImpl
	config.ListenPort = options.ListenPort
	config.NoDHT = !options.EnableDHT
	config.NoDefaultPortForwarding = !options.EnableUPnP && !options.EnableNATPMP
	config.EstablishedConnsPerTorrent = options.ConnectionsLimit
	config.Seed = true
	if options.DownloadRateLimit > 0 {
		config.DownloadRateLimiter = rate.NewLimiter(rate.Limit(options.DownloadRateLimit), 0)
	}
	if options.UploadRateLimit > 0 {
		config.UploadRateLimiter = rate.NewLimiter(rate.Limit(options.UploadRateLimit), 0)
	}
	if err := applyTorrentProxy(config, task.Stage.ProxyURL); err != nil {
		_ = storageImpl.Close()
		return nil, err
	}
	applyTorrentHeaders(config, task.Stage.Headers)
	webTransport := newManagedWebTransport(config)
	config.WebTransport = webTransport

	client, err := newClientWithPortFallback(config)
	if err != nil {
		webTransport.Close()
		_ = storageImpl.Close()
		return nil, fmt.Errorf("create BitTorrent client: %w", err)
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		closeTorrentClient(client, webTransport)
		_ = storageImpl.Close()
		return nil, fmt.Errorf("build BitTorrent spec: %w", err)
	}
	if trackers, err := TrackersFromTask(task); err != nil {
		closeTorrentClient(client, webTransport)
		_ = storageImpl.Close()
		return nil, err
	} else if len(trackers) > 0 {
		spec.Trackers = [][]string{trackers}
	}
	handle, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		closeTorrentClient(client, webTransport)
		_ = storageImpl.Close()
		return nil, fmt.Errorf("add torrent: %w", err)
	}

	engineFiles := handle.Files()
	selected := make(map[int]struct{})
	filesByIndex := make(map[int]*torrent.File, len(engineFiles))
	finalPaths := make(map[int]string, len(engineFiles))
	expectedSize := make(map[int]int64, len(engineFiles))
	metaFiles := info.UpvertedFiles()
	for index, engineFile := range engineFiles {
		filesByIndex[index] = engineFile
		if index < len(metaFiles) {
			finalPaths[index] = filepath.Join(task.Path, paths[metainfoFileKey(metaFiles[index])])
			expectedSize[index] = metaFiles[index].Length
		}
		engineFile.SetPriority(torrent.PiecePriorityNone)
	}
	for _, file := range files {
		if !file.Selected {
			continue
		}
		engineFile := filesByIndex[file.Index]
		if engineFile == nil {
			closeTorrentClient(client, webTransport)
			_ = storageImpl.Close()
			return nil, fmt.Errorf("torrent file index %d is unavailable", file.Index)
		}
		selected[file.Index] = struct{}{}
	}
	if len(selected) == 0 {
		closeTorrentClient(client, webTransport)
		_ = storageImpl.Close()
		return nil, errors.New("BitTorrent task has no selected files")
	}

	result := &torrentTransfer{
		client:       client,
		torrent:      handle,
		storage:      storageImpl,
		webTransport: webTransport,
		completion:   completion,
		filesByIndex: filesByIndex,
		selected:     selected,
		finalPaths:   finalPaths,
		expectedSize: expectedSize,
	}
	if options.SequentialDownload {
		sequentialCtx, cancel := context.WithCancel(ctx)
		result.sequentialCancel = cancel
		result.sequentialDone = make(chan error, 1)
		go result.downloadSequentially(sequentialCtx, files)
	} else {
		for index := range selected {
			filesByIndex[index].Download()
		}
	}
	if task.Stage.State[stateSourceType] == "magnet" && options.SaveMagnetTorrentFile {
		if err := saveMetainfo(filepath.Join(task.Path, task.Title+".torrent"), mi); err != nil {
			_ = result.Close()
			return nil, err
		}
	}
	return result, nil
}

func (t *torrentTransfer) downloadSequentially(ctx context.Context, files []File) {
	var runErr error
	defer func() { t.sequentialDone <- runErr }()
	for _, file := range files {
		if !file.Selected {
			continue
		}
		reader := t.filesByIndex[file.Index].NewReader()
		reader.SetContext(ctx)
		reader.SetReadahead(2 << 20)
		_, err := io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			runErr = err
			return
		}
		if closeErr != nil {
			runErr = closeErr
			return
		}
		if err := ctx.Err(); err != nil {
			return
		}
	}
}

func (t *torrentTransfer) Snapshot() (transferSnapshot, error) {
	if t.sequentialDone != nil {
		select {
		case err := <-t.sequentialDone:
			t.sequentialDone = nil
			if err != nil {
				return transferSnapshot{}, fmt.Errorf("sequential BitTorrent download: %w", err)
			}
		default:
		}
	}
	result := transferSnapshot{FileBytes: make(map[int]int64, len(t.selected))}
	for index := range t.selected {
		completed := verifiedFileBytes(t.filesByIndex[index])
		expected := t.expectedSize[index]
		if expected > 0 && completed >= expected {
			// Piece completion is published just before the .part file is
			// atomically promoted. Do not expose 100% until that promotion has
			// completed, otherwise a task can enter seeding while its final path
			// is not ready yet (and Windows can still have it open).
			info, err := os.Stat(t.finalPaths[index])
			switch {
			case err == nil && info.Mode().IsRegular() && info.Size() == expected:
			case errors.Is(err, os.ErrNotExist):
				completed = expected - 1
			case err != nil:
				return transferSnapshot{}, fmt.Errorf("check completed BitTorrent file: %w", err)
			default:
				completed = expected - 1
			}
		}
		result.FileBytes[index] = completed
	}
	stats := t.torrent.Stats()
	result.SessionDownloaded = stats.BytesReadUsefulData.Int64()
	result.SessionUploaded = stats.BytesWrittenData.Int64()
	result.Peers = stats.ActivePeers
	result.Seeds = stats.ConnectedSeeders
	return result, nil
}

func verifiedFileBytes(file *torrent.File) (completed int64) {
	for _, piece := range file.State() {
		if piece.Ok && piece.Complete && piece.Err == nil {
			completed += piece.Bytes
		}
	}
	return min64(completed, file.Length())
}

func (t *torrentTransfer) Close() error {
	t.closeOnce.Do(func() {
		if t.sequentialCancel != nil {
			t.sequentialCancel()
		}
		if t.sequentialDone != nil {
			// The sequential reader owns storage reads. Let it release them before
			// the torrent client and the durable completion database are closed.
			select {
			case err := <-t.sequentialDone:
				if err != nil {
					t.closeErr = errors.Join(t.closeErr, err)
				}
				t.sequentialDone = nil
			case <-time.After(5 * time.Second):
				t.closeErr = errors.Join(t.closeErr, errors.New("timed out stopping sequential BitTorrent reader"))
			}
		}
		if t.webTransport != nil {
			t.webTransport.Close()
		}
		if t.client != nil {
			for _, err := range t.client.Close() {
				if err != nil {
					t.closeErr = errors.Join(t.closeErr, err)
				}
			}
		}
		if t.completion != nil {
			t.closeErr = errors.Join(t.closeErr, t.completion.WaitIdle(250*time.Millisecond, 5*time.Second))
		}
		if t.storage != nil {
			t.closeErr = errors.Join(t.closeErr, t.storage.Close())
		}
	})
	return t.closeErr
}

type managedWebTransport struct {
	base   http.RoundTripper
	cancel context.CancelFunc
	ctx    context.Context

	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
	once    sync.Once
}

type trackedPieceCompletion struct {
	inner torrentstorage.PieceCompletion

	mu         sync.Mutex
	active     int
	lastAccess time.Time
}

func newTrackedPieceCompletion(inner torrentstorage.PieceCompletion) *trackedPieceCompletion {
	return &trackedPieceCompletion{inner: inner, lastAccess: time.Now()}
}

func (c *trackedPieceCompletion) begin() {
	c.mu.Lock()
	c.active++
	c.lastAccess = time.Now()
	c.mu.Unlock()
}

func (c *trackedPieceCompletion) end() {
	c.mu.Lock()
	c.active--
	c.lastAccess = time.Now()
	c.mu.Unlock()
}

func (c *trackedPieceCompletion) Get(key metainfo.PieceKey) (torrentstorage.Completion, error) {
	c.begin()
	defer c.end()
	return c.inner.Get(key)
}

func (c *trackedPieceCompletion) Set(key metainfo.PieceKey, complete bool) error {
	c.begin()
	defer c.end()
	return c.inner.Set(key, complete)
}

func (c *trackedPieceCompletion) Persistent() bool { return true }

func (c *trackedPieceCompletion) WaitIdle(quietPeriod, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		active := c.active
		idleFor := time.Since(c.lastAccess)
		c.mu.Unlock()
		if active == 0 && idleFor >= quietPeriod {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out draining BitTorrent piece checkpoint operations")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *trackedPieceCompletion) Close() error { return c.inner.Close() }

type trackedResponseBody struct {
	io.ReadCloser
	finish func()
}

func (b *trackedResponseBody) Read(data []byte) (int, error) {
	read, err := b.ReadCloser.Read(data)
	if err != nil {
		b.finish()
	}
	return read, err
}

func (b *trackedResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish()
	return err
}

func newManagedWebTransport(config *torrent.ClientConfig) *managedWebTransport {
	base := config.WebTransport
	if base == nil {
		transport := &http.Transport{
			Proxy:               config.HTTPProxy,
			DialContext:         config.HTTPDialContext,
			ForceAttemptHTTP2:   true,
			MaxConnsPerHost:     10,
			TLSHandshakeTimeout: 10 * time.Second,
		}
		if transport.DialContext == nil {
			transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		}
		base = transport
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &managedWebTransport{base: base, ctx: ctx, cancel: cancel}
}

func (t *managedWebTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		return nil, context.Canceled
	}
	t.wg.Add(1)
	t.mu.Unlock()

	requestCtx, cancel := context.WithCancel(request.Context())
	stopGlobalCancel := context.AfterFunc(t.ctx, cancel)
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			stopGlobalCancel()
			cancel()
			t.wg.Done()
		})
	}
	response, err := t.base.RoundTrip(request.Clone(requestCtx))
	if err != nil {
		finish()
		return nil, err
	}
	if response.Body == nil {
		finish()
		return response, nil
	}
	response.Body = &trackedResponseBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

func (t *managedWebTransport) Close() {
	t.once.Do(func() {
		t.mu.Lock()
		t.closing = true
		t.cancel()
		t.mu.Unlock()
		if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
		t.wg.Wait()
	})
}

func closeTorrentClient(client *torrent.Client, webTransport *managedWebTransport) {
	if webTransport != nil {
		webTransport.Close()
	}
	if client != nil {
		client.Close()
	}
}

func storagePaths(task core.Task, info metainfo.Info, files []File) (map[string]string, error) {
	title := strings.TrimSpace(task.Title)
	if title == "" || safeName(title, "") != title || filepath.Base(title) != title {
		return nil, fmt.Errorf("unsafe BitTorrent task title %q", task.Title)
	}
	displayByIndex := make(map[int]File, len(files))
	for _, file := range files {
		displayByIndex[file.Index] = file
	}
	metaFiles := info.UpvertedFiles()
	result := make(map[string]string, len(metaFiles))
	used := make(map[string]int, len(metaFiles))
	for index, metaFile := range metaFiles {
		var relative string
		if file, ok := displayByIndex[index]; ok && file.Selected {
			if info.IsDir() {
				relative = filepath.Join(title, filepath.FromSlash(file.Path))
			} else {
				relative = title
			}
		} else if ok {
			relative = filepath.Join(title, ".gd3_unselected", strconv.Itoa(index))
		} else {
			relative = filepath.Join(title, ".gd3_padding", strconv.Itoa(index))
		}
		clean := filepath.Clean(relative)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("torrent file %d escapes the task root", index)
		}
		collisionKey := strings.ToLower(clean)
		if previous, exists := used[collisionKey]; exists {
			return nil, fmt.Errorf("torrent files %d and %d map to the same output path %q", previous, index, clean)
		}
		used[collisionKey] = index
		result[metainfoFileKey(metaFile)] = clean
	}
	return result, nil
}

func metainfoFileKey(file metainfo.FileInfo) string {
	return strconv.FormatInt(file.TorrentOffset, 10) + ":" + strconv.FormatInt(file.Length, 10) + ":" + strings.Join(file.BestPath(), "/")
}

func applyTorrentProxy(config *torrent.ClientConfig, proxyURL string) error {
	text := strings.TrimSpace(proxyURL)
	if text == "" {
		return nil
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return fmt.Errorf("invalid BitTorrent proxy URL: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		config.HTTPProxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(parsed, proxy.Direct)
		if err != nil {
			return fmt.Errorf("invalid BitTorrent SOCKS proxy: %w", err)
		}
		dialContext := func(ctx context.Context, network, address string) (net.Conn, error) {
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				return contextDialer.DialContext(ctx, network, address)
			}
			type result struct {
				connection net.Conn
				err        error
			}
			results := make(chan result, 1)
			go func() {
				connection, err := dialer.Dial(network, address)
				results <- result{connection, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case result := <-results:
				return result.connection, result.err
			}
		}
		config.HTTPDialContext = dialContext
		config.TrackerDialContext = dialContext
	default:
		return fmt.Errorf("unsupported BitTorrent proxy scheme %q", parsed.Scheme)
	}
	return nil
}

func applyTorrentHeaders(config *torrent.ClientConfig, headers map[string]string) {
	if len(headers) == 0 {
		return
	}
	values := make(http.Header, len(headers))
	for name, value := range headers {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		values.Set(name, value)
	}
	if len(values) == 0 {
		return
	}
	config.HttpRequestDirector = func(request *http.Request) error {
		for name, entries := range values {
			request.Header.Del(name)
			for _, value := range entries {
				request.Header.Add(name, value)
			}
		}
		return nil
	}
	config.WebsocketTrackerHttpHeader = func() http.Header {
		return values.Clone()
	}
}

func newClientWithPortFallback(config *torrent.ClientConfig) (*torrent.Client, error) {
	client, err := torrent.NewClient(config)
	if err == nil || config.ListenPort == 0 || !addressAlreadyInUse(err) {
		return client, err
	}
	requestedPort := config.ListenPort
	fallback := *config
	fallback.ListenPort = 0
	client, fallbackErr := torrent.NewClient(&fallback)
	if fallbackErr != nil {
		return nil, fmt.Errorf("bind BitTorrent port %d: %w (ephemeral fallback: %v)", requestedPort, err, fallbackErr)
	}
	slog.Warn("BitTorrent listen port already in use; selected an ephemeral port", "requestedPort", requestedPort)
	return client, nil
}

func addressAlreadyInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.Errno(10048) // WSAEADDRINUSE
}

func saveMetainfo(path string, mi *metainfo.MetaInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create magnet torrent file: %w", err)
	}
	writeErr := mi.Write(file)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write magnet torrent file: %w", errors.Join(writeErr, closeErr))
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit magnet torrent file: %w", err)
	}
	return nil
}

func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
