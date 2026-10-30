// SPDX-License-Identifier: AGPL-3.0-or-later

package ratelimit

import (
	"testing"
	"time"
)

// fakeClock lets tests move time forward explicitly.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newLimiter(p Policy) (*Limiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	l := New(p)
	l.now = clock.now
	return l, clock
}

func TestAllowUpToBurstThenRefill(t *testing.T) {
	l, clock := newLimiter(Policy{Burst: 3, Every: 10 * time.Second})

	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d within burst was refused", i+1)
		}
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("request beyond burst was allowed")
	}
	if retry <= 0 || retry > 10*time.Second {
		t.Errorf("retryAfter = %v, want (0, 10s]", retry)
	}

	// A refused request doesn't consume a token: one refill period later, exactly
	// one more request is allowed.
	clock.advance(10 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("request after refill was refused")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("second request after a single refill was allowed")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l, _ := newLimiter(Policy{Burst: 1, Every: time.Minute})
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first request for a refused")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("second request for a allowed")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("first request for b refused because of a")
	}
}

func TestDisabledPolicyAllowsEverything(t *testing.T) {
	l, _ := newLimiter(Policy{})
	for range 1000 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("disabled policy refused a request")
		}
	}
	if l.size() != 0 {
		t.Errorf("disabled policy tracked %d buckets, want 0", l.size())
	}
}

func TestIdleBucketsAreForgottenOnlyAfterRefilling(t *testing.T) {
	p := Policy{Burst: 2, Every: time.Minute}
	l, clock := newLimiter(p)
	l.Allow("a")
	l.Allow("a")

	// Before the bucket could have refilled, it must be kept, or forgetting it
	// would reset the limit.
	// 90s refills 1.5 tokens: one request is allowed, the next isn't.
	clock.advance(90 * time.Second)
	l.Allow("b") // triggers a sweep
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("request after partial refill was refused")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("bucket was reset by the sweep before it had refilled")
	}

	clock.advance(p.idleAfter() + time.Second)
	l.Allow("c")
	if l.size() != 1 {
		t.Errorf("tracked %d buckets after idle sweep, want 1 (only c)", l.size())
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	for d, want := range map[time.Duration]int{
		10 * time.Millisecond:   1,
		time.Second:             1,
		1500 * time.Millisecond: 2,
		2 * time.Minute:         120,
	} {
		if got := RetryAfterSeconds(d); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %d, want %d", d, got, want)
		}
	}
}
