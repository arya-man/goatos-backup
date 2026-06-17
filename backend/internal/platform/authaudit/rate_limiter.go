package authaudit

import (
	"sync"
	"time"
)

const defaultRateLimiterMaxKeys = 4096

type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time
	buckets map[string]rateBucket
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func NewRateLimiter(limit int, window time.Duration, maxKeys int) *RateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	if maxKeys <= 0 {
		maxKeys = defaultRateLimiterMaxKeys
	}
	return &RateLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		now:     time.Now,
		buckets: map[string]rateBucket{},
	}
}

func (l *RateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.trimExpired(now)
	if len(l.buckets) >= l.maxKeys {
		l.dropOldest()
	}

	bucket := l.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= l.window {
		l.buckets[key] = rateBucket{windowStart: now, count: 1}
		return true
	}
	if bucket.count >= l.limit {
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (l *RateLimiter) trimExpired(now time.Time) {
	for key, bucket := range l.buckets {
		if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= l.window {
			delete(l.buckets, key)
		}
	}
}

func (l *RateLimiter) dropOldest() {
	var oldestKey string
	var oldest time.Time
	for key, bucket := range l.buckets {
		if oldestKey == "" || bucket.windowStart.Before(oldest) {
			oldestKey = key
			oldest = bucket.windowStart
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}
