package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBudgetPubSubFreshnessAndLowerForecastReminder(t *testing.T) {
	posts := 0
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts++; w.WriteHeader(200) }))
	defer slack.Close()
	now := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	start := now
	s := &server{token: "test-token", http: slack.Client(), webhook: slack.URL, log: slog.New(slog.NewTextHandler(io.Discard, nil)), budgetStore: newMemoryBudgetStore(), now: func() time.Time { return now }}
	send := func(published time.Time, forecast float64, wantPosts int) {
		t.Helper()
		b, _ := decodeBudgetNotification([]byte(repeatedBudget))
		b.ForecastThreshold = flexibleFloat(forecast)
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		push := pubsubPushForTest(string(data))
		push.Message.PublishAt = published.Format(time.RFC3339Nano)
		push.Message.Attributes = map[string]string{"budgetId": "b", "billingAccountId": "a"}
		data, err = json.Marshal(push)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.budgetPubsub(w, httptest.NewRequest("POST", "/budget-pubsub?token=test-token", strings.NewReader(string(data))))
		if w.Code != 204 || posts != wantPosts {
			t.Fatalf("published=%v forecast=%v status=%d posts=%d want=%d", published, forecast, w.Code, posts, wantPosts)
		}
	}
	send(start, 1, 1)
	now = start.Add(25 * time.Hour)
	send(start, .7777777778, 1) // duplicate publication cannot become a reminder
	send(now, .7777777778, 2)   // fresh lower forecast after 25h must remind
	now = now.Add(time.Hour)
	send(now, .5555555556, 2) // suppressed new observation advances freshness
	now = now.Add(25 * time.Hour)
	send(start.Add(25*time.Hour), 1.2, 2) // older escalation cannot bypass freshness
	send(now, .5555555556, 3)             // fresh still-lower update reminds
	now = now.Add(time.Hour)
	send(now, 0, 3) // recovery is recorded even though it does not send
	now = now.Add(25 * time.Hour)
	send(now.Add(-time.Hour), 0, 3)
	send(start.Add(52*time.Hour), 1, 3) // delayed warning older than recovery
	send(now, .7777777778, 4)
}

func TestBudgetFreshRetryAfterSlackFailure(t *testing.T) {
	fail := true
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer slack.Close()
	now := time.Now()
	s := &server{http: slack.Client(), webhook: slack.URL, budgetStore: newMemoryBudgetStore(), now: func() time.Time { return now }}
	b, _ := decodeBudgetNotification([]byte(repeatedBudget))
	b.PublishedAt = now
	if _, err := s.deliverBudget(context.Background(), b); err == nil {
		t.Fatal("failed send was acknowledged")
	}
	fail = false
	if sent, err := s.deliverBudget(context.Background(), b); !sent || err != nil {
		t.Fatalf("same publication retry sent=%v err=%v", sent, err)
	}
	now = now.Add(25 * time.Hour)
	if sent, err := s.deliverBudget(context.Background(), b); sent || err != nil {
		t.Fatalf("completed publication replay sent=%v err=%v", sent, err)
	}
}

func TestBudgetInvalidPublishTimeRejected(t *testing.T) {
	push := pubsubPushForTest(repeatedBudget)
	push.Message.PublishAt = "bad-date"
	data, _ := json.Marshal(push)
	if _, err := decodeBudgetNotification(data); err == nil {
		t.Fatal("invalid publish time accepted")
	}
}

func TestReviewFreshLowerForecastReminder(t *testing.T) {
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer slack.Close()
	now := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	s := &server{http: slack.Client(), webhook: slack.URL, budgetStore: newMemoryBudgetStore(), now: func() time.Time { return now }}
	b, _ := decodeBudgetNotification([]byte(repeatedBudget))
	if sent, err := s.deliverBudget(context.Background(), b); !sent || err != nil {
		t.Fatal(sent, err)
	}
	now = now.Add(25 * time.Hour)
	b.ForecastThreshold = .7777777778
	b.CostAmount = 35000
	if sent, err := s.deliverBudget(context.Background(), b); !sent || err != nil {
		t.Fatalf("fresh lower forecast sent=%v err=%v", sent, err)
	}
}
