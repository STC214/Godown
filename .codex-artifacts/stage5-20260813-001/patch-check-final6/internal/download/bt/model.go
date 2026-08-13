package btdownload

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/anacrolix/torrent/metainfo"

	"ghost-downloader-go-win32/internal/core"
)

const (
	stateMetainfo            = "metainfo"
	stateSourceType          = "sourceType"
	stateTrackers            = "trackers"
	stateFiles               = "files"
	stateMetadataTimeout     = "metadataTimeoutSec"
	stateListenPort          = "listenPort"
	stateConnectionsLimit    = "connectionsLimit"
	stateDownloadRate        = "downloadRateLimit"
	stateUploadRate          = "uploadRateLimit"
	stateEnableDHT           = "enableDHT"
	stateEnableLSD           = "enableLSD"
	stateEnableUPnP          = "enableUPnP"
	stateEnableNATPMP        = "enableNATPMP"
	stateSequential          = "sequentialDownload"
	stateSeedRatio           = "seedRatioLimitPercent"
	stateSeedTime            = "seedTimeLimitMinutes"
	stateSaveMagnet          = "saveMagnetTorrentFile"
	stateUploadedBytes       = "uploadedBytes"
	stateSeedingSeconds      = "seedingTimeSeconds"
	statePhase               = "phase"
	statePeerCount           = "peerCount"
	stateSeedCount           = "seedCount"
	stateCurrentDownloadRate = "downloadRate"
	stateCurrentUploadRate   = "uploadRate"
	stateShareRatio          = "shareRatioPercent"
)

// File is a selectable, non-padding file in a BitTorrent task. Index is the
// original metainfo file index and remains stable when padding files are skipped.
type File struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Selected   bool   `json:"selected"`
	Priority   int    `json:"priority"`
	Downloaded int64  `json:"downloadedBytes,omitempty"`
	Completed  bool   `json:"completed,omitempty"`
}

// RuntimeOptions is the worker-facing, persisted subset of Options.
type RuntimeOptions struct {
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
	SaveMagnetTorrentFile bool
}

// RuntimeState is the durable transfer checkpoint emitted by Worker.
type RuntimeState struct {
	Files          []File
	UploadedBytes  int64
	SeedingSeconds int64
	Phase          string
	PeerCount      int
	SeedCount      int
	DownloadRate   int64
	UploadRate     int64
	ShareRatio     float64
}

// RuntimeStateFromTask decodes the current transfer checkpoint.
func RuntimeStateFromTask(task core.Task) (RuntimeState, error) {
	files, err := FilesFromTask(task)
	if err != nil {
		return RuntimeState{}, err
	}
	state := task.Stage.State
	shareRatio, _ := strconv.ParseFloat(state[stateShareRatio], 64)
	return RuntimeState{
		Files:          files,
		UploadedBytes:  int64State(state, stateUploadedBytes, 0),
		SeedingSeconds: int64State(state, stateSeedingSeconds, 0),
		Phase:          state[statePhase],
		PeerCount:      intState(state, statePeerCount, 0),
		SeedCount:      intState(state, stateSeedCount, 0),
		DownloadRate:   int64State(state, stateCurrentDownloadRate, 0),
		UploadRate:     int64State(state, stateCurrentUploadRate, 0),
		ShareRatio:     shareRatio,
	}, nil
}

// OutputPath returns the primary shell target for a BitTorrent task.
// Multi-file tasks map to the task root directory; single-file tasks map to the file.
func OutputPath(task core.Task) (string, error) {
	if _, err := FilesFromTask(task); err != nil {
		return "", err
	}
	return filepath.Join(task.Path, task.Title), nil
}

func runtimeStatePatch(state RuntimeState) (map[string]string, error) {
	files, err := json.Marshal(state.Files)
	if err != nil {
		return nil, fmt.Errorf("encode BitTorrent file checkpoint: %w", err)
	}
	return map[string]string{
		stateFiles:               string(files),
		stateUploadedBytes:       strconv.FormatInt(state.UploadedBytes, 10),
		stateSeedingSeconds:      strconv.FormatInt(state.SeedingSeconds, 10),
		statePhase:               state.Phase,
		statePeerCount:           strconv.Itoa(state.PeerCount),
		stateSeedCount:           strconv.Itoa(state.SeedCount),
		stateCurrentDownloadRate: strconv.FormatInt(state.DownloadRate, 10),
		stateCurrentUploadRate:   strconv.FormatInt(state.UploadRate, 10),
		stateShareRatio:          strconv.FormatFloat(state.ShareRatio, 'f', 4, 64),
	}, nil
}

// FilesFromTask decodes the persisted file selection.
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

// SetSelectedFiles updates a task copy with the selected original metainfo indexes.
// At least one downloadable file must remain selected.
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
	var selectedCount int
	for i := range files {
		_, files[i].Selected = selected[files[i].Index]
		if files[i].Selected {
			files[i].Priority = 4
			total += files[i].Size
			selectedCount++
		} else {
			files[i].Priority = 0
			files[i].Downloaded = 0
			files[i].Completed = false
		}
	}
	if selectedCount == 0 {
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

// DecodeMetainfo returns the persisted bencoded metainfo for a worker.
func DecodeMetainfo(task core.Task) (*metainfo.MetaInfo, error) {
	if task.Stage.State == nil {
		return nil, errors.New("BitTorrent task has no state")
	}
	encoded := strings.TrimSpace(task.Stage.State[stateMetainfo])
	if encoded == "" {
		return nil, errors.New("BitTorrent task has no metainfo")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode BitTorrent metainfo base64: %w", err)
	}
	mi, err := metainfo.Load(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode BitTorrent metainfo: %w", err)
	}
	return mi, nil
}

// TrackersFromTask returns the persisted, normalized tracker list.
func TrackersFromTask(task core.Task) ([]string, error) {
	if task.Stage.State == nil || strings.TrimSpace(task.Stage.State[stateTrackers]) == "" {
		return nil, nil
	}
	var trackers []string
	if err := json.Unmarshal([]byte(task.Stage.State[stateTrackers]), &trackers); err != nil {
		return nil, fmt.Errorf("decode BitTorrent trackers: %w", err)
	}
	return trackers, nil
}

// RuntimeOptionsFromTask decodes the runtime settings used by a future worker.
func RuntimeOptionsFromTask(task core.Task) RuntimeOptions {
	state := task.Stage.State
	return RuntimeOptions{
		MetadataTimeout:       time.Duration(intState(state, stateMetadataTimeout, 30)) * time.Second,
		ListenPort:            intState(state, stateListenPort, 0),
		ConnectionsLimit:      intState(state, stateConnectionsLimit, 500),
		DownloadRateLimit:     int64State(state, stateDownloadRate, 0),
		UploadRateLimit:       int64State(state, stateUploadRate, 0),
		EnableDHT:             boolState(state, stateEnableDHT, true),
		EnableLSD:             boolState(state, stateEnableLSD, true),
		EnableUPnP:            boolState(state, stateEnableUPnP, true),
		EnableNATPMP:          boolState(state, stateEnableNATPMP, true),
		SequentialDownload:    boolState(state, stateSequential, false),
		SeedRatioLimitPercent: intState(state, stateSeedRatio, 0),
		SeedTimeLimitMinutes:  intState(state, stateSeedTime, 0),
		SaveMagnetTorrentFile: boolState(state, stateSaveMagnet, false),
	}
}

func filesFromInfo(info metainfo.Info) ([]File, int64, error) {
	metaFiles := info.UpvertedFiles()
	files := make([]File, 0, len(metaFiles))
	var total int64
	for index := range metaFiles {
		metaFile := metaFiles[index]
		if strings.Contains(metaFile.Attr, "p") {
			continue
		}
		var rawParts []string
		if info.IsDir() {
			rawParts = metaFile.BestPath()
		} else {
			rawParts = []string{info.BestName()}
		}
		cleanPath, err := safeRelativePath(rawParts)
		if err != nil {
			return nil, 0, fmt.Errorf("unsafe torrent file %d: %w", index, err)
		}
		if metaFile.Length < 0 {
			return nil, 0, fmt.Errorf("torrent file %d has negative size", index)
		}
		files = append(files, File{Index: index, Path: cleanPath, Size: metaFile.Length, Selected: true, Priority: 4})
		total += metaFile.Length
	}
	if len(files) == 0 {
		return nil, 0, errors.New("torrent contains no downloadable regular files")
	}
	return files, total, nil
}

func safeRelativePath(parts []string) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("empty path")
	}
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, "\\", "/"))
		for _, nested := range strings.Split(part, "/") {
			nested = strings.TrimSpace(nested)
			if nested == "" || nested == "." || nested == ".." || isDriveComponent(nested) {
				return "", fmt.Errorf("invalid path component %q", nested)
			}
			nested = safeName(nested, "file")
			if nested == "" || nested == "." || nested == ".." {
				return "", fmt.Errorf("invalid path component %q", nested)
			}
			safe = append(safe, nested)
		}
	}
	joined := path.Clean(strings.Join(safe, "/"))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") || strings.HasPrefix(joined, "/") {
		return "", errors.New("path escapes torrent root")
	}
	return joined, nil
}

func isDriveComponent(value string) bool {
	return len(value) == 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}
func safeName(value, fallback string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\\|?*`, r) {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(r)
		}
	}
	value = strings.Trim(builder.String(), ". ")
	if value == "" || value == "." || value == ".." {
		return fallback
	}
	return value
}

func boolText(value bool) string { return strconv.FormatBool(value) }
func boolState(state map[string]string, key string, fallback bool) bool {
	if parsed, err := strconv.ParseBool(state[key]); err == nil {
		return parsed
	}
	return fallback
}
func intState(state map[string]string, key string, fallback int) int {
	if parsed, err := strconv.Atoi(state[key]); err == nil {
		return parsed
	}
	return fallback
}
func int64State(state map[string]string, key string, fallback int64) int64 {
	if parsed, err := strconv.ParseInt(state[key], 10, 64); err == nil {
		return parsed
	}
	return fallback
}
