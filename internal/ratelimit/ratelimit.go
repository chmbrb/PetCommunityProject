// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ratelimit provides in-memory, per-key token-bucket rate limiting.
//
// Buckets live in the app process, which is enough for the single-host deployment
// (ADR 0009). Running several app replicas would need a shared store instead.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Policy is a token bucket: up to Burst requests at once, refilled at one request
// per Every. A zero Burst disables limiting.
type Policy struct {
	Burst int
	Every time.Duration
}

// Disabled reports whether the policy allows everything.
func (p Policy) Disabled() bool { return p.Burst <= 0 }

// idleAfter is how long an unused bucket is kept. It must be longer than the time a
// bucket takes to refill completely, so forgetting it never resets a limit early.
func (p Policy) idleAfter() time.Duration {
	refill := time.Duration(p.Burst) * p.Every
	return max(refill, time.Minute) + time.Minute
}

// Limiter tracks one bucket per key under a single policy.
type Limiter struct {
	policy Policy
	now    func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// New returns a limiter for the policy.
func New(p Policy) *Limiter {
	return &Limiter{policy: p, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow takes one token from key's bucket. If the bucket is empty it returns false
// and how long until a token is available.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	if l.policy.Disabled() {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)
	b, found := l.buckets[key]
	if !found {
		b = &bucket{limiter: rate.NewLimiter(rate.Every(l.policy.Every), l.policy.Burst)}
		l.buckets[key] = b
	}
	b.lastUsed = now

	r := b.limiter.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// RetryAfterSeconds rounds a delay up to whole seconds for the Retry-After header.
func RetryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}

// sweep forgets idle buckets, at most once a minute. Callers hold l.mu.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	idle := l.policy.idleAfter()
	for key, b := range l.buckets {
		if now.Sub(b.lastUsed) > idle {
			delete(l.buckets, key)
		}
	}
}

// size reports how many buckets are tracked (for tests).
func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
