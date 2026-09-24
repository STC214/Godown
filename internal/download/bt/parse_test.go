package btdownload

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func makeTorrent(t *testing.T, info metainfo.Info, announce string, tiers metainfo.AnnounceList) []byte {
	t.Helper()
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, Announce: announce, AnnounceList: tiers}
	var buffer bytes.Buffer
	if err := mi.Write(&buffer); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type v2InfoFixture struct {
	Name        string             `bencode:"name"`
	PieceLength int64              `bencode:"piece length"`
	MetaVersion int64              `bencode:"meta version"`
	FileTree    *metainfo.FileTree `bencode:"file tree"`
}

func makeV2Torrent(t *testing.T, info v2InfoFixture, urlList metainfo.UrlList) []byte {
	t.Helper()
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, UrlList: urlList}
	var buffer bytes.Buffer
	if err := mi.Write(&buffer); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeTorrent(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.torrent")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsSource(t *testing.T) {
	validHash := strings.Repeat("a", 40)
	cases := map[string]bool{
		"magnet:?xt=urn:btih:" + validHash: true,
		"magnet:?xt=urn:btmh:abc":          false,
		"magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e": true,
		`C:\downloads\sample.torrent`:        true,
		"file:///C:/sample.torrent":          true,
		"https://example.test/a.torrent?q=1": true,
		"https://example.test/a.bin":         false,
		"":                                   false,
	}
	for source, want := range cases {
		if got := IsSource(source); got != want {
			t.Errorf("IsSource(%q)=%v, want %v", source, got, want)
		}
	}
}

func TestResolveSingleFileAndDecode(t *testing.T) {
	data := makeTorrent(t, metainfo.Info{Name: `bad:name?.mp4`, PieceLength: 16, Length: 123}, "udp://tracker.one:80/announce", nil)
	path := writeTorrent(t, data)
	task, err := Resolve(context.Background(), path, Options{
		DownloadDir:           t.TempDir(),
		MetadataTimeout:       7 * time.Second,
		ListenPort:            6881,
		ConnectionsLimit:      19,
		DownloadRateLimit:     1000,
		UploadRateLimit:       2000,
		EnableDHT:             true,
		EnableLSD:             true,
		EnableUPnP:            true,
		EnableNATPMP:          true,
		SequentialDownload:    true,
		SeedRatioLimitPercent: 120,
		SeedTimeLimitMinutes:  5,
		SaveMagnetTorrentFile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.PackID != "bt" || task.Title != "bad_name_.mp4" || task.FileSize != 123 {
		t.Fatalf("unexpected task: %#v", task)
	}
	files, err := FilesFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []File{{Index: 0, Path: "bad_name_.mp4", Size: 123, Selected: true, Priority: 2}}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("files=%#v, want %#v", files, wantFiles)
	}
	decoded, err := DecodeMetainfo(task)
	if err != nil {
		t.Fatal(err)
	}
	info, err := decoded.UnmarshalInfo()
	if err != nil || info.BestName() != `bad:name?.mp4` {
		t.Fatalf("decoded info=%#v err=%v", info, err)
	}
	runtime := RuntimeOptionsFromTask(task)
	if runtime.MetadataTimeout != 7*time.Second || runtime.ListenPort != 6881 || runtime.ConnectionsLimit != 19 || !runtime.EnableNATPMP || !runtime.SequentialDownload || runtime.SeedRatioLimitPercent != 120 || !runtime.SaveMagnetTorrentFile {
		t.Fatalf("unexpected runtime options: %#v", runtime)
	}
	if task.Stage.State[stateSourceType] != "torrent" {
		t.Fatalf("sourceType=%q", task.Stage.State[stateSourceType])
	}
	stored, err := base64.StdEncoding.DecodeString(task.Stage.State[stateMetainfo])
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored metainfo mismatch: err=%v", err)
	}
}

func TestResolveFileURL(t *testing.T) {
	path := writeTorrent(t, makeTorrent(t, metainfo.Info{Name: "file.bin", PieceLength: 16, Length: 5}, "", nil))
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	task, err := Resolve(context.Background(), fileURL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if task.FileSize != 5 {
		t.Fatalf("size=%d", task.FileSize)
	}
}

func TestResolveMultiFileSkipsPadAndSelection(t *testing.T) {
	info := metainfo.Info{Name: "root", PieceLength: 16, Files: []metainfo.FileInfo{
		{Length: 10, Path: []string{"video.mp4"}},
		{Length: 6, Path: []string{".pad", "6"}, ExtendedFileAttrs: metainfo.ExtendedFileAttrs{Attr: "p"}},
		{Length: 20, Path: []string{"sub", "audio.m4a"}},
	}}
	task, err := Resolve(context.Background(), writeTorrent(t, makeTorrent(t, info, "", nil)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Index != 0 || files[1].Index != 2 || task.FileSize != 30 {
		t.Fatalf("files=%#v size=%d", files, task.FileSize)
	}
	selected, err := SetSelectedFiles(task, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	files, _ = FilesFromTask(selected)
	if files[0].Selected || files[0].Priority != 0 || !files[1].Selected || files[1].Priority != 2 || selected.FileSize != 20 || selected.Stage.FileSize != 20 {
		t.Fatalf("selection task=%#v files=%#v", selected, files)
	}
	originalFiles, _ := FilesFromTask(task)
	if !originalFiles[0].Selected || task.FileSize != 30 {
		t.Fatalf("SetSelectedFiles mutated original: task=%#v files=%#v", task, originalFiles)
	}
	if _, err := SetSelectedFiles(task, nil); err == nil {
		t.Fatal("empty selection succeeded")
	}
	if _, err := SetSelectedFiles(task, []int{99}); err == nil {
		t.Fatal("unknown-only selection succeeded")
	}
}

func TestResolveRejectsPathTraversal(t *testing.T) {
	for _, parts := range [][]string{{"..", "evil.exe"}, {"C:", "evil.exe"}, {"safe", `..\evil.exe`}} {
		info := metainfo.Info{Name: "root", PieceLength: 16, Files: []metainfo.FileInfo{{Length: 1, Path: parts}}}
		_, err := Resolve(context.Background(), writeTorrent(t, makeTorrent(t, info, "", nil)), Options{})
		if err == nil || !strings.Contains(err.Error(), "unsafe torrent file") {
			t.Fatalf("parts=%q err=%v", parts, err)
		}
	}
}

func TestResolveRejectsWindowsPathCollisionAfterSanitizing(t *testing.T) {
	info := metainfo.Info{Name: "root", PieceLength: 16, Files: []metainfo.FileInfo{
		{Length: 1, Path: []string{"a:b.bin"}},
		{Length: 1, Path: []string{"A?B.bin"}},
	}}
	_, err := Resolve(context.Background(), writeTorrent(t, makeTorrent(t, info, "", nil)), Options{})
	if err == nil || !strings.Contains(err.Error(), "same output path") {
		t.Fatalf("sanitized path collision was accepted: %v", err)
	}
}

func TestTrackerMergeDeduplicates(t *testing.T) {
	data := makeTorrent(t, metainfo.Info{Name: "file", PieceLength: 16, Length: 1}, "udp://one.test:80/announce", metainfo.AnnounceList{
		{"udp://one.test:80/announce", "https://two.test/announce"},
	})
	task, err := Resolve(context.Background(), writeTorrent(t, data), Options{ExtraTrackers: []string{
		"https://two.test/announce udp://three.test:80/announce",
		"javascript:bad https://two.test/announce",
	}})
	if err != nil {
		t.Fatal(err)
	}
	trackers, err := TrackersFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"udp://one.test:80/announce", "https://two.test/announce", "udp://three.test:80/announce"}
	if !reflect.DeepEqual(trackers, want) {
		t.Fatalf("trackers=%q want=%q", trackers, want)
	}
}

func TestResolveMagnetTimeoutAndCancellation(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)
	blocking := func(ctx context.Context, _ string, _ Options) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	_, err := Resolve(context.Background(), magnet, Options{MetadataTimeout: 20 * time.Millisecond, MagnetResolver: blocking})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("timeout err=%v elapsed=%v", err, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Resolve(ctx, magnet, Options{MetadataTimeout: time.Hour, MagnetResolver: blocking})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestResolveV2OnlyMagnetUsesResolverAndTrackers(t *testing.T) {
	root := strings.Repeat("r", 32)
	v2Info := v2InfoFixture{
		Name:        "v2-fixture",
		PieceLength: 16,
		MetaVersion: 2,
		FileTree: &metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			"v2-fixture.bin": {File: metainfo.FileTreeFile{Length: 16, PiecesRoot: root}},
		}},
	}
	metadata := makeV2Torrent(t, v2Info, nil)
	mi, err := loadMetainfoBytes(metadata)
	if err != nil {
		t.Fatal(err)
	}
	v2Magnet, err := mi.MagnetV2()
	if err != nil {
		t.Fatal(err)
	}
	v2Magnet.Trackers = []string{"https://tracker.test/announce"}
	magnet := v2Magnet.String()
	called := false
	task, err := Resolve(context.Background(), magnet, Options{
		DownloadDir: t.TempDir(),
		MagnetResolver: func(_ context.Context, source string, _ Options) ([]byte, error) {
			called = source == magnet
			return metadata, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trackers, err := TrackersFromTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if !called || task.Stage.State[stateSourceType] != "magnet" || !reflect.DeepEqual(trackers, []string{"https://tracker.test/announce"}) {
		t.Fatalf("v2 magnet was not preserved: task=%#v trackers=%q", task, trackers)
	}
	decoded, err := DecodeMetainfo(task)
	if err != nil {
		t.Fatal(err)
	}
	info, err := decoded.UnmarshalInfo()
	if err != nil || !info.HasV2() || info.HasV1() {
		t.Fatalf("resolved metadata is not v2-only: info=%#v err=%v", info, err)
	}
}
