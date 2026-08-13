package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu        sync.Mutex
	rate      int64
	allowance float64
	last      time.Time
}

func New(rateBytesPerSecond int64) *Limiter {
	return &Limiter{
		rate:      rateBytesPerSecond,
		allowance: float64(rateBytesPerSecond),
		last:      time.Now(),
	}
}

func (l *Limiter) SetRate(rateBytesPerSecond int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rateBytesPerSecond < 0 {
		rateBytesPerSecond = 0
	}
	l.refillLocked(time.Now())
	l.rate = rateBytesPerSecond
	if l.rate == 0 {
		l.allowance = 0
		return
	}
	if l.allowance > float64(l.rate) {
		l.allowance = float64(l.rate)
	}
}

func (l *Limiter) Rate() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

func (l *Limiter) Wait(ctx context.Context, bytes int) error {
	if l == nil || bytes <= 0 {
		return nil
	}
	need := float64(bytes)
	for {
		l.mu.Lock()
		rate := l.rate
		if rate <= 0 {
			l.mu.Unlock()
			return nil
		}
		now := time.Now()
		l.refillLocked(now)
		if l.allowance >= need {
			l.allowance -= need
			l.mu.Unlock()
			return nil
		}
		deficit := need - l.allowance
		wait := time.Duration(deficit / float64(rate) * float64(time.Second))
		if wait < 10*time.Millisecond {
			wait = 10 * time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *Limiter) refillLocked(now time.Time) {
	if l.last.IsZero() {
		l.last = now
		return
	}
	if l.rate <= 0 {
		l.last = now
		return
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		return
	}
	l.allowance += elapsed * float64(l.rate)
	if l.allowance > float64(l.rate) {
		l.allowance = float64(l.rate)
	}
	l.last = now
}
