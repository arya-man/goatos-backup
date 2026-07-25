package memcache

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func TestSetGet(t *testing.T) {
	c := New(time.Minute, 4)
	c.Set("k", domain.Answer{Answer: "hi"})
	got, ok := c.Get("k")
	if !ok || got.Answer != "hi" {
		t.Fatalf("expected hit, got %v %v", got, ok)
	}
}

func TestTTLExpiry(t *testing.T) {
	c := New(time.Hour, 4)
	base := time.Now()
	c.now = func() time.Time { return base }
	c.Set("k", domain.Answer{Answer: "hi"})
	c.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected expiry")
	}
}

func TestLRUEviction(t *testing.T) {
	c := New(time.Hour, 2)
	c.Set("a", domain.Answer{})
	c.Set("b", domain.Answer{})
	c.Set("c", domain.Answer{}) // evicts a
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be evicted")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("c should be present")
	}
}
