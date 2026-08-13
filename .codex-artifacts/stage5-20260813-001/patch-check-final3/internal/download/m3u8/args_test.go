package m3u8download

import (
	"reflect"
	"testing"
)

func TestBuildArgsVOD(t *testing.T) {
	args := Options{
		URL:                    "https://example.test/master.m3u8",
		SaveDir:                `C:\Downloads`,
		Title:                  "movie.mp4",
		TaskID:                 "tsk_1",
		FFmpegPath:             `C:\Tools\ffmpeg.exe`,
		ProxyURL:               "socks5h://127.0.0.1:1080",
		Headers:                map[string]string{"Referer": "https://example.test/", "Empty": ""},
		SelectAllAudioSubtitle: true,
		CheckSegmentsCount:     true,
		DeleteAfterDone:        true,
		MP4RealTimeDecryption:  true,
	}.BuildArgs()

	assertContains(t, args, "--custom-proxy=socks5://127.0.0.1:1080")
	assertContains(t, args, "--save-name=movie")
	assertContains(t, args, "--tmp-dir=C:/Downloads/.gd3_m3u8/tsk_1")
	assertContains(t, args, "--mux-after-done=format=mp4:muxer=ffmpeg:bin_path=C:/Tools/ffmpeg.exe")
	assertContains(t, args, "-H")
	assertContains(t, args, "Referer: https://example.test/")
	if !reflect.DeepEqual(args[:2], []string{"https://example.test/master.m3u8", "--save-dir=C:/Downloads"}) {
		t.Fatalf("unexpected leading args: %#v", args[:2])
	}
}

func TestBuildArgsLive(t *testing.T) {
	args := Options{
		URL:                   "https://example.test/live.m3u8",
		SaveDir:               `D:\Live`,
		Title:                 "live.ts",
		IsLive:                true,
		LiveKeepSegments:      true,
		LiveRecordLimit:       "00:30:00",
		CheckSegmentsCount:    true,
		DeleteAfterDone:       true,
		MP4RealTimeDecryption: true,
	}.BuildArgs()
	assertContains(t, args, "--live-real-time-merge=true")
	assertContains(t, args, "--live-keep-segments=true")
	assertContains(t, args, "--live-record-limit=00:30:00")
}

func assertContains(t *testing.T, items []string, want string) {
	t.Helper()
	for _, item := range items {
		if item == want {
			return
		}
	}
	t.Fatalf("missing %q in %#v", want, items)
}
