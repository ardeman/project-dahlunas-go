package web

import (
	"sync"
	"time"
)

// checkoutLimiter bounds work per handler instance without trusting proxy headers
// or retaining buyer identifiers. Ten requests can burst; one refills every 2s.
type checkoutLimiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func (l *checkoutLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last.IsZero() {
		l.tokens = 10
	} else {
		l.tokens = min(10, l.tokens+max(0, now.Sub(l.last).Seconds())/2)
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
