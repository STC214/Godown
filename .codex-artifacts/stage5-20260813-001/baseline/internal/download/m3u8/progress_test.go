package m3u8download

import "testing"

func TestParseVODProgressLine(t *testing.T) {
	progress := ParseProgressLine("12/24 50.00% 10.00MB/20.00MB 2.50MBps eta")
	if !progress.Matched {
		t.Fatal("expected VOD progress to match")
	}
	if progress.Progress != 50 {
		t.Fatalf("progress=%v", progress.Progress)
	}
	if progress.Received != 10*1024*1024 || progress.Total != 20*1024*1024 || progress.Speed != int64(2.5*1024*1024) {
		t.Fatalf("unexpected byte fields: %#v", progress)
	}
}

func TestParseLiveProgressLine(t *testing.T) {
	progress := ParseProgressLine("00m10s/00m30s 4/8 Recording 33% 1.00MBps")
	if !progress.Matched {
		t.Fatal("expected live progress to match")
	}
	if progress.LiveStatus != "Recording" || progress.LiveElapsed != "00m10s" || progress.LiveTotal != "00m30s" {
		t.Fatalf("unexpected live fields: %#v", progress)
	}
	if progress.Speed != 1024*1024 {
		t.Fatalf("speed=%d", progress.Speed)
	}
}
