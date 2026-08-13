package ffmpegdownload

import (
	"testing"

	"ghost-downloader-go-win32/internal/config"
)

func TestNewMergeTask(t *testing.T) {
	task, err := NewMergeTask("Page: Title", `D:\Downloads`, []MergeResource{
		{URL: "https://example.test/video.mp4", Filename: "video.mp4", Size: 10},
		{URL: "https://example.test/audio.m4a", Filename: "audio.m4a", Size: 5},
	}, config.Settings{FFmpegInstallDir: `C:\FFmpeg`, RetryCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if task.PackID != "ffmpeg" || task.Title != "Page_ Title.mp4" || task.FileSize != 15 {
		t.Fatalf("task=%#v", task)
	}
	resources, err := ResourcesFromState(task)
	if err != nil {
		t.Fatal(err)
	}
	if resources[0].Role != "video" || resources[1].Role != "audio" {
		t.Fatalf("resources=%#v", resources)
	}
}
