package m3u8download

import "testing"

func TestIsManifestSource(t *testing.T) {
	cases := map[string]bool{
		"https://example.test/video.m3u8":         true,
		"https://example.test/api?file=video.mpd": true,
		"C:/media/local.m3u8":                     true,
		"file:///C:/media/local.mpd":              true,
		"https://example.test/file.zip":           false,
		"magnet:?xt=urn:btih:abc":                 false,
	}
	for input, want := range cases {
		if got := IsManifestSource(input); got != want {
			t.Fatalf("IsManifestSource(%q)=%v want %v", input, got, want)
		}
	}
}

func TestDetectManifestTypeAndLive(t *testing.T) {
	if got := DetectManifestType("https://example.test/live.mpd", nil, ""); got != ManifestDASH {
		t.Fatalf("DASH by suffix got %q", got)
	}
	if got := DetectManifestType("https://example.test/live", map[string]string{"Content-Type": "application/dash+xml"}, ""); got != ManifestDASH {
		t.Fatalf("DASH by content-type got %q", got)
	}
	if !IsLiveManifest(ManifestDASH, `<MPD type="dynamic"></MPD>`) {
		t.Fatal("dynamic MPD should be live")
	}
	if IsLiveManifest(ManifestHLS, "#EXTM3U\n#EXT-X-ENDLIST") {
		t.Fatal("HLS with ENDLIST should not be live")
	}
}

func TestTitleFrom(t *testing.T) {
	title := TitleFrom(
		"https://example.test/path/ignored.m3u8",
		map[string]string{"Content-Disposition": `attachment; filename="movie:01.m3u8"`},
		"mp4",
	)
	if title != "movie_01.mp4" {
		t.Fatalf("title=%q", title)
	}
	if got := TitleFrom("https://example.test/play?title=episode.ts", nil, "mkv"); got != "episode.mkv" {
		t.Fatalf("query title=%q", got)
	}
}
