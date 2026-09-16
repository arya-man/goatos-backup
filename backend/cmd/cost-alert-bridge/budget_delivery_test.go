package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const repeatedBudget = `{"budgetDisplayName":"monthly","costIntervalStart":"2026-09-01T00:00:00Z","costAmount":33677.52,"budgetAmount":45000,"forecastThresholdExceeded":1,"currencyCode":"INR"}`

func TestBudgetRepeatedUpdatesPostOnce(t *testing.T) {
	posts := 0
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts++; w.WriteHeader(200) }))
	defer slack.Close()
	s := &server{http: slack.Client(), webhook: slack.URL, token: "test-token", log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now, budgetStore: newMemoryBudgetStore()}
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		s.budgetPubsub(w, httptest.NewRequest("POST", "/budget-pubsub?token=test-token", strings.NewReader(repeatedBudget)))
		if w.Code != 204 {
			t.Fatalf("status = %d", w.Code)
		}
	}
	if posts != 1 {
		t.Fatalf("three same-threshold updates posted %d Slack messages; want 1", posts)
	}
}

type memoryBudgetStore struct {
	mu       sync.Mutex
	states   map[string]budgetState
	versions map[string]int64
	fail     bool
}

func newMemoryBudgetStore() *memoryBudgetStore {
	return &memoryBudgetStore{states: map[string]budgetState{}, versions: map[string]int64{}}
}
func (m *memoryBudgetStore) Load(_ context.Context, key string) (budgetState, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return budgetState{}, 0, errors.New("store unavailable")
	}
	return m.states[key], m.versions[key], nil
}
func (m *memoryBudgetStore) Save(_ context.Context, key string, version int64, s budgetState) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return 0, errors.New("store unavailable")
	}
	if version != m.versions[key] {
		return 0, errStateConflict
	}
	m.versions[key]++
	m.states[key] = s
	return m.versions[key], nil
}

func TestBudgetDeliveryPolicy(t *testing.T) {
	var posts atomic.Int32
	var fail atomic.Bool
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer slack.Close()
	now := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	store := newMemoryBudgetStore()
	makeServer := func() *server {
		return &server{http: slack.Client(), webhook: slack.URL, budgetStore: store, now: func() time.Time { return now }}
	}
	s := makeServer()
	b, err := decodeBudgetNotification([]byte(repeatedBudget))
	if err != nil {
		t.Fatal(err)
	}
	send := func(want bool) {
		t.Helper()
		got, err := s.deliverBudget(context.Background(), b)
		if err != nil || got != want {
			t.Fatalf("sent=%v err=%v want=%v", got, err, want)
		}
	}
	b.ForecastThreshold = .55
	send(true)
	s = makeServer() // a new process with the same durable store
	b.CostAmount = 33760.12
	send(false)
	now = now.Add(23 * time.Hour)
	send(false)
	now = now.Add(time.Hour)
	send(true)
	b.ForecastThreshold = .77
	send(true)
	b.ForecastThreshold = .55
	send(false) // same-day previously announced threshold
	b.ForecastThreshold = 1
	send(true)
	b.AlertThresholdExceeded = .9
	send(true) // actual is separate from forecast
	now = now.Add(24 * time.Hour)
	b.ForecastThreshold = .55
	send(true) // a fresh lower forecast still gets its daily reminder
	b.ForecastThreshold = 1
	b.CostIntervalStart = "2026-10-01T00:00:00Z"
	send(true)
	b.BudgetID = "another-budget"
	send(true)
	if posts.Load() != 8 {
		t.Fatalf("posts=%d want=8", posts.Load())
	}
	b.ForecastThreshold = 1.1
	fail.Store(true)
	if _, err := s.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("Slack failure acknowledged")
	}
	fail.Store(false)
	send(true)
	send(false)
	store.fail = true
	if _, err := s.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("store failure acknowledged")
	}
	if posts.Load() != 10 {
		t.Fatalf("store failure posted to Slack: posts=%d", posts.Load())
	}
}

func TestBudgetConcurrentInstancesAndLeaseRecovery(t *testing.T) {
	var posts atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if posts.Add(1) == 1 {
			close(entered)
			<-release
		}
		w.WriteHeader(200)
	}))
	defer slack.Close()
	now := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	store := newMemoryBudgetStore()
	s := &server{http: slack.Client(), webhook: slack.URL, budgetStore: store, now: func() time.Time { return now }}
	b, _ := decodeBudgetNotification([]byte(repeatedBudget))
	done := make(chan error, 1)
	go func() { _, err := s.deliverBudget(context.Background(), b); done <- err }()
	<-entered
	other := *s
	if _, err := other.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("active lease should retry")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if sent, err := other.deliverBudget(context.Background(), b); sent || err != nil {
		t.Fatalf("duplicate sent=%v err=%v", sent, err)
	}
	if posts.Load() != 1 {
		t.Fatalf("concurrent posts=%d", posts.Load())
	}
	b.BudgetID = "crashed-worker"
	key, _ := b.stateKey()
	_, err := store.Save(context.Background(), key, 0, budgetState{LeaseUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if sent, err := s.deliverBudget(context.Background(), b); !sent || err != nil {
		t.Fatalf("expired lease sent=%v err=%v", sent, err)
	}
}

func TestBudgetNoThresholdAndMissingStateDoNotPost(t *testing.T) {
	s := &server{now: time.Now}
	b, _ := decodeBudgetNotification([]byte(repeatedBudget))
	if _, err := s.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("missing store must retry")
	}
	b.ForecastThreshold = 0
	if sent, err := s.deliverBudget(context.Background(), b); sent || err != nil {
		t.Fatalf("below threshold sent=%v err=%v", sent, err)
	}
	b.ForecastThreshold = 1
	b.CostIntervalStart = ""
	if _, err := s.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("missing interval must not bypass dedup")
	}
}

func TestBudgetActualEscalationIsVisible(t *testing.T) {
	s := &server{}
	b, _ := decodeBudgetNotification([]byte(repeatedBudget))
	b.AlertThresholdExceeded = .9
	msg := s.formatBudget(b)
	if !strings.Contains(msg, "forecast crossed 100.0%") || !strings.Contains(msg, "actual spend crossed 90.0%") {
		t.Fatalf("missing alert basis: %s", msg)
	}
}

func TestBudgetStoreFailureRetriesThroughHTTP(t *testing.T) {
	store := newMemoryBudgetStore()
	store.fail = true
	s := &server{token: "test-token", budgetStore: store, now: time.Now, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	w := httptest.NewRecorder()
	s.budgetPubsub(w, httptest.NewRequest("POST", "/budget-pubsub?token=test-token", strings.NewReader(repeatedBudget)))
	if w.Code != 503 {
		t.Fatalf("store failure status=%d want=503", w.Code)
	}
}

func TestBudgetEnvelopeRetainsStableIdentity(t *testing.T) {
	push := pubsubPushForTest(repeatedBudget)
	push.Message.Attributes = map[string]string{"budgetId": "budget-1", "billingAccountId": "account-1"}
	data, err := json.Marshal(push)
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeBudgetNotification(data)
	if err != nil {
		t.Fatal(err)
	}
	key, err := b.stateKey()
	if err != nil {
		t.Fatal(err)
	}
	b.BudgetDisplayName = "renamed"
	renamed, _ := b.stateKey()
	if key != renamed {
		t.Fatal("display-name change reset ID-keyed suppression")
	}
	b.BillingAccountID = "account-2"
	another, _ := b.stateKey()
	if key == another {
		t.Fatal("billing accounts share suppression")
	}
}

type conflictingBudgetStore struct{ loads, saves int }

func (s *conflictingBudgetStore) Load(context.Context, string) (budgetState, int64, error) {
	s.loads++
	return budgetState{}, 0, nil
}
func (s *conflictingBudgetStore) Save(context.Context, string, int64, budgetState) (int64, error) {
	s.saves++
	return 0, errStateConflict
}

func TestBudgetContentionHasBoundedRetries(t *testing.T) {
	store := &conflictingBudgetStore{}
	s := &server{budgetStore: store, now: time.Now}
	b, err := decodeBudgetNotification([]byte(repeatedBudget))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := s.deliverBudget(context.Background(), b)
	if sent || !errors.Is(err, errStateConflict) {
		t.Fatalf("contention sent=%v err=%v", sent, err)
	}
	if store.loads != 5 || store.saves != 5 {
		t.Fatalf("unbounded contention: loads=%d saves=%d", store.loads, store.saves)
	}
}
