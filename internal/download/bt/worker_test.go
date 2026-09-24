package btdownload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"ghost-downloader-go-win32/internal/core"
)

type fakeTransfer struct {
	mu        sync.Mutex
	snapshots []transferSnapshot
	index     int
	closed    bool
}

func TestEngineFilePriorityMapsPersistedLevels(t *testing.T) {
	if engineFilePriority(1) != torrent.PiecePriorityNormal ||
		engineFilePriority(2) != torrent.PiecePriorityHigh ||
		engineFilePriority(3) != torrent.PiecePriorityReadahead ||
		engineFilePriority(4) != torrent.PiecePriorityNormal {
		t.Fatal("persisted BitTorrent priority mapping changed")
	}
}

func TestSequentialModeDoesNotRequestInactiveFiles(t *testing.T) {
	for _, priority := range []int{1, 2, 3} {
		if got := requestedFilePriority(priority, true); got != torrent.PiecePriorityNone {
			t.Fatalf("sequential file priority %d requested inactive pieces: %v", priority, got)
		}
		if got := requestedFilePriority(priority, false); got == torrent.PiecePriorityNone {
			t.Fatalf("normal file priority %d did not request pieces", priority)
		}
	}
}

func TestSequentialFilesHonorPriorityAndStableOrder(t *testing.T) {
	files := []File{
		{Index: 0, Selected: true, Priority: 1},
		{Index: 1, Selected: true, Priority: 3},
		{Index: 2, Selected: false, Priority: 3},
		{Index: 3, Selected: true, Priority: 2},
		{Index: 4, Selected: true, Priority: 3},
	}
	ordered := sequentialFiles(files)
	indexes := make([]int, len(ordered))
	for index, file := range ordered {
		indexes[index] = file.Index
	}
	if !reflect.DeepEqual(indexes, []int{1, 4, 3, 0}) {
		t.Fatalf("sequential priority order=%v", indexes)
	}
	if files[0].Index != 0 || files[1].Index != 1 {
		t.Fatalf("sequential ordering mutated source: %#v", files)
	}
}

func loopbackClientConfig(config *torrent.ClientConfig) {
	config.ListenHost = func(string) string { return "127.0.0.1" }
	config.DisableIPv6 = true
	config.NoDHT = true
	config.NoDefaultPortForwarding = true
}

func loopbackRuntimeOptions(task core.Task) RuntimeOptions {
	options := RuntimeOptionsFromTask(task)
	options.testListenHost = func(string) string { return "127.0.0.1" }
	options.testDisableIPv6 = true
	return options
}

func (f *fakeTransfer) Snapshot() (transferSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.snapshots) == 0 {
		return transferSnapshot{}, errors.New("no fake transfer snapshots")
	}
	index := f.index
	if index >= len(f.snapshots) {
		index = len(f.snapshots) - 1
	}
	result := f.snapshots[index]
	if f.index < len(f.snapshots)-1 {
		f.index++
	}
	return result, nil
}

func (f *fakeTransfer) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func makeWorkerTask(t *testing.T, files []File, ratioLimit int, timeLimit int) core.Task {
	t.Helper()
	encoded, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	stage := core.NewStage("bt", "fixture.torrent", selectedSize(files), 1, 0, false, nil, "")
	stage.State = map[string]string{
		stateFiles:            string(encoded),
		stateSeedRatio:        strconv.Itoa(ratioLimit),
		stateSeedTime:         strconv.Itoa(timeLimit),
		stateMetadataTimeout:  "30",
		stateConnectionsLimit: "500",
	}
	return core.NewTask("bt", "fixture", "fixture.torrent", t.TempDir(), selectedSize(files), stage)
}

func TestWorkerReportsSelectionCheckpointAndStopsAtRatio(t *testing.T) {
	files := []File{{Index: 0, Path: "a.bin", Size: 100, Selected: true, Priority: 4}}
	task := makeWorkerTask(t, files, 50, 0)
	active := &fakeTransfer{snapshots: []transferSnapshot{
		{FileBytes: map[int]int64{0: 40}, SessionDownloaded: 40},
		{FileBytes: map[int]int64{0: 100}, SessionDownloaded: 100, SessionUploaded: 50, Peers: 3, Seeds: 2},
	}}
	var updates []core.ProgressUpdate
	worker := Worker{
		tick: 2 * time.Millisecond,
		newTransfer: func(_ context.Context, got core.Task, gotFiles []File, _ RuntimeOptions) (transfer, error) {
			if got.ID != task.ID || !reflect.DeepEqual(gotFiles, files) {
				t.Fatalf("unexpected transfer input task=%q files=%#v", got.ID, gotFiles)
			}
			return active, nil
		},
	}
	if err := worker.Run(context.Background(), task, func(update core.ProgressUpdate) {
		updates = append(updates, update)
	}); err != nil {
		t.Fatal(err)
	}
	if len(updates) < 3 {
		t.Fatalf("updates=%d, want initial and two samples", len(updates))
	}
	last := updates[len(updates)-1]
	if last.Received != 100 || last.Progress != 100 || last.UsesSlot == nil || *last.UsesSlot {
		t.Fatalf("unexpected final update: %#v", last)
	}
	if last.Status != core.StatusSeeding {
		t.Fatalf("completed content did not enter seeding status: %#v", last)
	}
	if last.StageState[statePhase] != "seeding" || last.StageState[stateShareRatio] != "50.0000" || last.StageState[statePeerCount] != "3" {
		t.Fatalf("unexpected final state: %#v", last.StageState)
	}
	if !strings.Contains(last.Detail, "BT seeding") || !strings.Contains(last.Detail, "peers 3") || !strings.Contains(last.Detail, "ratio 50.00%") {
		t.Fatalf("unexpected runtime detail: %q", last.Detail)
	}
	var checkpointFiles []File
	if err := json.Unmarshal([]byte(last.StageState[stateFiles]), &checkpointFiles); err != nil {
		t.Fatal(err)
	}
	if len(checkpointFiles) != 1 || !checkpointFiles[0].Completed || checkpointFiles[0].Downloaded != 100 {
		t.Fatalf("checkpoint files=%#v", checkpointFiles)
	}
	if !active.closed {
		t.Fatal("transfer was not closed")
	}
}

func TestWorkerCancellationSavesPausedDownload(t *testing.T) {
	files := []File{{Index: 2, Path: "video.bin", Size: 100, Selected: true, Priority: 4}}
	task := makeWorkerTask(t, files, 0, 0)
	active := &fakeTransfer{snapshots: []transferSnapshot{{
		FileBytes: map[int]int64{2: 25}, SessionDownloaded: 25,
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var updates []core.ProgressUpdate
	done := make(chan error, 1)
	go func() {
		done <- (Worker{
			tick: 2 * time.Millisecond,
			newTransfer: func(context.Context, core.Task, []File, RuntimeOptions) (transfer, error) {
				return active, nil
			},
		}).Run(ctx, task, func(update core.ProgressUpdate) {
			mu.Lock()
			updates = append(updates, update)
			mu.Unlock()
		})
	}()
	time.Sleep(8 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v", err)
	}
	mu.Lock()
	last := updates[len(updates)-1]
	mu.Unlock()
	if last.StageState[statePhase] != "paused_download" || last.Received != 25 || last.UsesSlot == nil || !*last.UsesSlot {
		t.Fatalf("unexpected paused update: %#v", last)
	}
	if !active.closed {
		t.Fatal("transfer was not closed")
	}
}

func TestShouldStopSeedingUsesEitherEnabledLimit(t *testing.T) {
	if !shouldStopSeeding(RuntimeOptions{SeedRatioLimitPercent: 100}, RuntimeState{ShareRatio: 100}) {
		t.Fatal("ratio limit did not stop seeding")
	}
	if !shouldStopSeeding(RuntimeOptions{SeedTimeLimitMinutes: 2}, RuntimeState{SeedingSeconds: 120}) {
		t.Fatal("time limit did not stop seeding")
	}
	if shouldStopSeeding(RuntimeOptions{}, RuntimeState{ShareRatio: 1000, SeedingSeconds: 1000}) {
		t.Fatal("disabled limits stopped seeding")
	}
}

func TestWorkerResetTaskClearsRuntimeCheckpointAndKeepsSelection(t *testing.T) {
	files := []File{
		{Index: 0, Path: "keep.bin", Size: 100, Selected: true, Priority: 4, Downloaded: 100, Completed: true},
		{Index: 2, Path: "skip.bin", Size: 50, Selected: false, Priority: 0},
	}
	task := makeWorkerTask(t, files, 100, 60)
	task.UsesSlot = false
	task.Stage.State[stateUploadedBytes] = "120"
	task.Stage.State[stateSeedingSeconds] = "45"
	task.Stage.State[statePhase] = "seeding"
	task.Stage.State[statePeerCount] = "4"
	task.Stage.State[stateSeedCount] = "2"
	task.Stage.State[stateCurrentDownloadRate] = "5"
	task.Stage.State[stateCurrentUploadRate] = "6"
	task.Stage.State[stateShareRatio] = "120.0000"

	reset, err := (Worker{}).ResetTask(task)
	if err != nil {
		t.Fatal(err)
	}
	state, err := RuntimeStateFromTask(reset)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 2 || !state.Files[0].Selected || state.Files[1].Selected {
		t.Fatalf("selection changed during reset: %#v", state.Files)
	}
	if state.Files[0].Downloaded != 0 || state.Files[0].Completed || state.UploadedBytes != 0 || state.SeedingSeconds != 0 || state.Phase != "" || state.ShareRatio != 0 {
		t.Fatalf("runtime checkpoint was not cleared: %#v", state)
	}
	if !reset.UsesSlot || reset.FileSize != 100 || reset.Stage.FileSize != 100 {
		t.Fatalf("reset task accounting is wrong: usesSlot=%v sizes=%d/%d", reset.UsesSlot, reset.FileSize, reset.Stage.FileSize)
	}
	original, err := RuntimeStateFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if original.Files[0].Downloaded != 100 || original.UploadedBytes != 120 {
		t.Fatalf("ResetTask mutated its input: %#v", original)
	}
}

func TestStoragePathsMapSelectedFilesAndPadding(t *testing.T) {
	info := metainfo.Info{Name: "original", PieceLength: 16, Files: []metainfo.FileInfo{
		{Length: 10, Path: []string{"video.mp4"}},
		{Length: 6, Path: []string{".pad", "6"}, ExtendedFileAttrs: metainfo.ExtendedFileAttrs{Attr: "p"}},
		{Length: 20, Path: []string{"sub", "audio.m4a"}},
	}}
	metaFiles := info.UpvertedFiles()
	paths, err := storagePaths(core.Task{Title: "renamed"}, info, []File{
		{Index: 0, Path: "video.mp4", Selected: true},
		{Index: 2, Path: "sub/audio.m4a", Selected: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		metainfoFileKey(metaFiles[0]): `renamed\video.mp4`,
		metainfoFileKey(metaFiles[1]): `renamed\.gd3_padding\1`,
		metainfoFileKey(metaFiles[2]): `renamed\.gd3_unselected\2`,
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths=%#v, want %#v", paths, want)
	}
}

func TestApplyTorrentHeadersCoversHTTPAndWebsocketRequests(t *testing.T) {
	config := torrent.NewDefaultClientConfig()
	applyTorrentHeaders(config, map[string]string{"Authorization": "Bearer fixture", "X-Trace": "stage5"})
	request, err := http.NewRequest(http.MethodGet, "https://example.invalid/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.HttpRequestDirector(request); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Authorization") != "Bearer fixture" || request.Header.Get("X-Trace") != "stage5" {
		t.Fatalf("HTTP headers not applied: %#v", request.Header)
	}
	websocketHeaders := config.WebsocketTrackerHttpHeader()
	websocketHeaders.Set("Authorization", "mutated")
	if config.WebsocketTrackerHttpHeader().Get("Authorization") != "Bearer fixture" {
		t.Fatal("websocket tracker headers were not cloned")
	}
}

func TestNewClientFallsBackWhenConfiguredPortIsBusy(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	newConfig := func() *torrent.ClientConfig {
		config := torrent.NewDefaultClientConfig()
		loopbackClientConfig(config)
		config.DataDir = t.TempDir()
		config.ListenPort = port
		config.NoDHT = true
		config.NoDefaultPortForwarding = true
		return config
	}
	first, err := newClientWithPortFallback(newConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newClientWithPortFallback(newConfig())
	if err != nil {
		t.Fatalf("second client did not use an ephemeral port: %v", err)
	}
	defer second.Close()
}

func TestTorrentTransferDownloadsFromLocalWebseedAndResumes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local BitTorrent integration test in short mode")
	}
	payload := bytes.Repeat([]byte("ghost-downloader-bt-fixture\n"), 4096)
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		http.ServeContent(response, request, "payload.bin", time.Unix(0, 0), bytes.NewReader(payload))
	}))
	server.Config.SetKeepAlivesEnabled(false)
	server.Start()
	defer server.Close()

	info := metainfo.Info{Name: "payload.bin", PieceLength: 16 << 10, Length: int64(len(payload))}
	if err := info.GeneratePieces(func(metainfo.FileInfo) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, UrlList: []string{server.URL + "/payload.bin"}}
	var torrentBytes bytes.Buffer
	if err := mi.Write(&torrentBytes); err != nil {
		t.Fatal(err)
	}
	metainfoPath := filepath.Join(t.TempDir(), "webseed.torrent")
	if err := os.WriteFile(metainfoPath, torrentBytes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	task, err := Resolve(context.Background(), metainfoPath, Options{
		DownloadDir:      downloadDir,
		ConnectionsLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(task)
	if err != nil {
		t.Fatal(err)
	}

	downloadOnce := func(timeout time.Duration) transferSnapshot {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		active, err := newTorrentTransfer(ctx, task, files, loopbackRuntimeOptions(task))
		if err != nil {
			t.Fatal(err)
		}
		defer active.Close()
		deadline := time.Now().Add(timeout)
		var last transferSnapshot
		for time.Now().Before(deadline) {
			snapshot, err := active.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.FileBytes[0] >= int64(len(payload)) {
				return snapshot
			}
			last = snapshot
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for local webseed download: last=%#v requests=%d", last, requests.Load())
		return transferSnapshot{}
	}

	first := downloadOnce(30 * time.Second)
	if first.FileBytes[0] != int64(len(payload)) || requests.Load() == 0 {
		t.Fatalf("first download snapshot=%#v requests=%d", first, requests.Load())
	}
	written, err := os.ReadFile(filepath.Join(downloadDir, task.Title))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, payload) {
		t.Fatal("downloaded payload mismatch")
	}

	requestCount := requests.Load()
	second := downloadOnce(5 * time.Second)
	if second.FileBytes[0] != int64(len(payload)) {
		t.Fatalf("resume snapshot=%#v", second)
	}
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != requestCount {
		t.Fatalf("resume fetched completed data again: before=%d after=%d", requestCount, requests.Load())
	}
}

func TestTorrentTransferDownloadsV2OnlyFromLocalWebseed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local BitTorrent v2 integration test in short mode")
	}
	payload := bytes.Repeat([]byte("v2-only-webseed-fixture\n"), 512)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.ServeContent(response, request, "v2.bin", time.Unix(0, 0), bytes.NewReader(payload))
	}))
	defer server.Close()
	root := sha256.Sum256(payload)
	metadata := makeV2Torrent(t, v2InfoFixture{
		Name:        "v2-root",
		PieceLength: 16 << 10,
		MetaVersion: 2,
		FileTree: &metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			"v2.bin": {File: metainfo.FileTreeFile{Length: int64(len(payload)), PiecesRoot: string(root[:])}},
		}},
	}, metainfo.UrlList{server.URL + "/"})
	metainfoPath := filepath.Join(t.TempDir(), "v2.torrent")
	if err := os.WriteFile(metainfoPath, metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	task, err := Resolve(context.Background(), metainfoPath, Options{DownloadDir: downloadDir, ConnectionsLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	active, err := newTorrentTransfer(context.Background(), task, files, loopbackRuntimeOptions(task))
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, snapshotErr := active.Snapshot()
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.FileBytes[0] >= int64(len(payload)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(downloadDir, task.Title, "v2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("v2-only WebSeed payload mismatch")
	}
}

type slowCountingResponseWriter struct {
	http.ResponseWriter
	phase        int32
	firstServed  *atomic.Int64
	secondServed *atomic.Int64
}

func (w slowCountingResponseWriter) Write(data []byte) (int, error) {
	time.Sleep(8 * time.Millisecond)
	written, err := w.ResponseWriter.Write(data)
	if w.phase == 1 {
		w.firstServed.Add(int64(written))
	} else {
		w.secondServed.Add(int64(written))
	}
	return written, err
}

func TestTorrentTransferResumesVerifiedPartialPieces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping local BitTorrent integration test in short mode")
	}
	payload := bytes.Repeat([]byte("partial-resume-fixture-"), 60000)
	var phase atomic.Int32
	var firstServed, secondServed atomic.Int64
	phase.Store(1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.ServeContent(
			slowCountingResponseWriter{response, phase.Load(), &firstServed, &secondServed},
			request,
			"partial.bin",
			time.Unix(0, 0),
			bytes.NewReader(payload),
		)
	}))
	server.Config.SetKeepAlivesEnabled(false)
	server.Start()
	defer server.Close()

	info := metainfo.Info{Name: "partial.bin", PieceLength: 32 << 10, Length: int64(len(payload))}
	if err := info.GeneratePieces(func(metainfo.FileInfo) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, UrlList: []string{server.URL + "/partial.bin"}}
	var torrentBytes bytes.Buffer
	if err := mi.Write(&torrentBytes); err != nil {
		t.Fatal(err)
	}
	metainfoPath := filepath.Join(t.TempDir(), "partial.torrent")
	if err := os.WriteFile(metainfoPath, torrentBytes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	task, err := Resolve(context.Background(), metainfoPath, Options{DownloadDir: downloadDir, ConnectionsLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(task)
	if err != nil {
		t.Fatal(err)
	}

	first, err := newTorrentTransfer(context.Background(), task, files, loopbackRuntimeOptions(task))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var pausedAt int64
	for time.Now().Before(deadline) {
		snapshot, snapshotErr := first.Snapshot()
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		pausedAt = snapshot.FileBytes[0]
		if pausedAt >= 128<<10 && pausedAt < int64(len(payload)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pausedAt < 128<<10 || pausedAt >= int64(len(payload)) {
		_ = first.Close()
		t.Fatalf("did not capture a partial checkpoint: %d/%d", pausedAt, len(payload))
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if firstServed.Load() <= 0 {
		t.Fatal("partial session served no data")
	}

	phase.Store(2)
	second, err := newTorrentTransfer(context.Background(), task, files, loopbackRuntimeOptions(task))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, snapshotErr := second.Snapshot()
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.FileBytes[0] >= int64(len(payload)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(downloadDir, task.Title))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, payload) {
		t.Fatal("resumed payload mismatch")
	}
	if secondServed.Load() <= 0 || secondServed.Load() >= int64(len(payload)) {
		t.Fatalf("resume did not reuse verified pieces: paused=%d firstServed=%d secondServed=%d total=%d", pausedAt, firstServed.Load(), secondServed.Load(), len(payload))
	}
}
