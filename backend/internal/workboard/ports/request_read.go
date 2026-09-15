package ports

import (
	"context"
	"sync"
)

type requestReadContextKey struct{}
type requestReadMemo struct {
	mu      sync.Mutex
	entries map[any]*requestReadEntry
}
type requestReadEntry struct {
	done  chan struct{}
	value any
	err   error
}
type requestReadValue[T any] struct{ value T }

type typedRequestReadKey[T any, K comparable] struct{ key K }

// WithRequestReadMemo creates a fresh request scope. Never retain this context
// beyond the HTTP request: successful reads are shared only within that scope.
func WithRequestReadMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestReadContextKey{}, &requestReadMemo{entries: make(map[any]*requestReadEntry)})
}

// HasRequestReadMemo reports whether a bundled page owns this request scope.
func HasRequestReadMemo(ctx context.Context) bool {
	_, ok := ctx.Value(requestReadContextKey{}).(*requestReadMemo)
	return ok
}

// RequestRead shares a typed successful read among concurrent callers in one
// request. Failures are evicted so a later lane can recover after a failed summary.
// A canceled waiter stops waiting without canceling another caller's read.
func RequestRead[T any, K comparable](ctx context.Context, key K, read func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	memo, ok := ctx.Value(requestReadContextKey{}).(*requestReadMemo)
	if !ok {
		return read(ctx)
	}
	typedKey := typedRequestReadKey[T, K]{key: key}
	memo.mu.Lock()
	if entry, exists := memo.entries[typedKey]; exists {
		memo.mu.Unlock()
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-entry.done:
			if entry.err != nil {
				return zero, entry.err
			}
			return entry.value.(requestReadValue[T]).value, nil
		}
	}
	entry := &requestReadEntry{done: make(chan struct{})}
	memo.entries[typedKey] = entry
	memo.mu.Unlock()
	value, err := read(ctx)
	memo.mu.Lock()
	entry.value, entry.err = requestReadValue[T]{value}, err
	if err != nil {
		delete(memo.entries, typedKey)
	}
	close(entry.done)
	memo.mu.Unlock()
	return value, err
}
