package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

type stateRoundTrip func(*http.Request) (*http.Response, error)

func (f stateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGCSBudgetStateGenerationContract(t *testing.T) {
	generation := int64(0)
	stored := budgetState{}
	client := &http.Client{Transport: stateRoundTrip(func(r *http.Request) (*http.Response, error) {
		code := 200
		body := ""
		if r.Method == "GET" {
			if generation == 0 {
				code = 404
				body = `{"error":{"code":404,"message":"missing"}}`
			} else if r.URL.Query().Get("alt") == "media" {
				if r.URL.Query().Get("generation") != fmt.Sprint(generation) {
					t.Errorf("unfenced read: %s", r.URL)
				}
				data, _ := json.Marshal(stored)
				body = string(data)
			} else {
				body = fmt.Sprintf(`{"generation":"%d"}`, generation)
			}
		} else if r.Method == "POST" {
			if r.URL.Query().Get("ifGenerationMatch") != fmt.Sprint(generation) {
				code = 412
				body = `{"error":{"code":412,"message":"precondition failed"}}`
			} else {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				// Multipart upload must include the durable state, not only object metadata.
				if !strings.Contains(string(raw), `"forecast":1`) {
					t.Errorf("upload missing state: %s", raw)
				}
				generation++
				body = fmt.Sprintf(`{"generation":"%d"}`, generation)
			}
		} else {
			t.Errorf("unexpected method: %s", r.Method)
		}
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	api, err := storage.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	g := &gcsBudgetStateStore{api: api, bucket: "test-state"}
	ctx := context.Background()
	state, gen, err := g.Load(ctx, "budget/key.json")
	if err != nil || gen != 0 || !state.LastSent.IsZero() {
		t.Fatalf("missing read %v %d %v", state, gen, err)
	}
	stored = budgetState{Forecast: 1, LastSent: time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)}
	gen, err = g.Save(ctx, "budget/key.json", gen, stored)
	if err != nil || gen != 1 {
		t.Fatalf("create %d %v", gen, err)
	}
	state, gen, err = g.Load(ctx, "budget/key.json")
	if err != nil || state != stored || gen != 1 {
		t.Fatalf("read %v %d %v", state, gen, err)
	}
	gen, err = g.Save(ctx, "budget/key.json", gen, stored)
	if err != nil || gen != 2 {
		t.Fatalf("replace %d %v", gen, err)
	}
	if _, err = g.Save(ctx, "budget/key.json", 1, stored); !errors.Is(err, errStateConflict) {
		t.Fatalf("stale write err=%v", err)
	}
}
