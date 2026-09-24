package btruntime

import (
	"math"
	"reflect"
	"testing"

	"ghost-downloader-go-win32/internal/core"
)

func TestIsSourceAndParseTrackers(t *testing.T) {
	for _, source := range []string{
		`C:\downloads\fixture.torrent`,
		"file:///C:/downloads/fixture.torrent",
		"https://example.test/fixture.torrent",
		"magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		"magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e",
	} {
		if !IsSource(source) {
			t.Fatalf("expected supported source %q", source)
		}
	}
	for _, source := range []string{"", "https://example.test/file.zip", "magnet:?dn=missing-hash"} {
		if IsSource(source) {
			t.Fatalf("unexpected supported source %q", source)
		}
	}
	want := []string{"udp://one.test:80/announce", "https://two.test/announce"}
	if got := ParseTrackers("udp://one.test:80/announce\nhttps://two.test/announce udp://one.test:80/announce invalid"); !reflect.DeepEqual(got, want) {
		t.Fatalf("trackers=%q want=%q", got, want)
	}
}

func TestSetSelectedFilesClonesTaskState(t *testing.T) {
	task := core.Task{Stage: core.Stage{State: map[string]string{
		stateFiles:  `[{"index":0,"path":"one.bin","size":10,"selected":true,"priority":4},{"index":2,"path":"two.bin","size":20,"selected":true,"priority":4}]`,
		"immutable": "yes",
	}}}
	updated, err := SetSelectedFiles(task, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(updated)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Selected || !files[1].Selected || updated.FileSize != 20 || updated.Stage.FileSize != 20 || updated.Received != 0 {
		t.Fatalf("updated task=%#v files=%#v", updated, files)
	}
	if task.Stage.State[stateFiles] == updated.Stage.State[stateFiles] || task.Stage.State["immutable"] != "yes" {
		t.Fatal("selection mutated the source task state")
	}
}

func TestSetFilePrioritiesPersistsLevelsAndClampsProgress(t *testing.T) {
	task := core.Task{Stage: core.Stage{State: map[string]string{
		stateFiles: `[{"index":0,"path":"one.bin","size":10,"selected":true,"priority":4,"downloadedBytes":20},{"index":2,"path":"two.bin","size":20,"selected":true,"priority":4}]`,
	}}}
	updated, err := SetFilePriorities(task, map[int]int{0: FilePriorityLow, 2: FilePriorityHigh})
	if err != nil {
		t.Fatal(err)
	}
	files, err := FilesFromTask(updated)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Priority != FilePriorityLow || files[1].Priority != FilePriorityHigh || files[0].Downloaded != 10 || updated.Received != 10 || math.Abs(updated.Progress-100.0/3.0) > 0.000001 {
		t.Fatalf("priority update mismatch: task=%#v files=%#v", updated, files)
	}
	if _, err := SetFilePriorities(task, map[int]int{0: 99}); err == nil {
		t.Fatal("invalid priority accepted")
	}
}
