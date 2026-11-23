package conad

import (
	"context"
	"sync"
	"time"
)

// limiter spaces requests: each waits for its turn, one gap after the one before.
type limiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func newLimiter(gap time.Duration) *limiter { return &limiter{gap: gap} }

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	// A turn that would come after the caller gives up is not taken: one slow burst of
	// requests must not push every later request past its own deadline.
	if dl, ok := ctx.Deadline(); ok && at.After(dl) {
		l.mu.Unlock()
		return context.DeadlineExceeded
	}
	l.next = at.Add(l.gap)
	l.mu.Unlock()
	d := at.Sub(now)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		l.mu.Lock() // the turn is given back when it is still the last one
		if l.next.Equal(at.Add(l.gap)) {
			l.next = at
		}
		l.mu.Unlock()
		return ctx.Err()
	}
}

// cache keeps what was read for a while, up to a number of entries.
type cache[V any] struct {
	mu  sync.Mutex
	m   map[string]entry[V]
	ttl time.Duration
	max int
}

type entry[V any] struct {
	v    V
	till time.Time
}

func newCache[V any](ttl time.Duration, max int) *cache[V] {
	return &cache[V]{m: map[string]entry[V]{}, ttl: ttl, max: max}
}

func (c *cache[V]) get(k string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || time.Now().After(e.till) {
		delete(c.m, k)
		var zero V
		return zero, false
	}
	return e.v, true
}

func (c *cache[V]) put(k string, v V) { c.putFor(k, v, c.ttl) }

// putFor keeps an entry for its own time, shorter than the cache's for what may be fixed soon.
func (c *cache[V]) putFor(k string, v V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		now := time.Now()
		for key, e := range c.m {
			if now.After(e.till) {
				delete(c.m, key)
			}
		}
		for key := range c.m { // still full: drop one
			if len(c.m) < c.max {
				break
			}
			delete(c.m, key)
		}
	}
	c.m[k] = entry[V]{v: v, till: time.Now().Add(ttl)}
}
