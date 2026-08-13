package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestLimiterUnlimitedReturnsImmediately(t *testing.T) {
	limiter := New(0)
	start := time.Now()
	if err := limiter.Wait(context.Background(), 1<<20); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("unlimited wait took too long: %s", elapsed)
	}
}

func TestLimiterRateCanChange(t *testing.T) {
	limiter := New(1024)
	if got := limiter.Rate(); got != 1024 {
		t.Fatalf("expected 1024, got %d", got)
	}
	limiter.SetRate(0)
	if got := limiter.Rate(); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}
