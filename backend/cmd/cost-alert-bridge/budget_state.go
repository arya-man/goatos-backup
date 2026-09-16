package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

var errStateConflict = errors.New("budget state changed concurrently")

// A single object per budget and billing interval provides a cross-instance fence.
// The lease bounds crash recovery; successful sends set the shared reminder clock.
type budgetState struct {
	LatestPublishedAt time.Time `json:"latest_published_at"`
	Forecast          float64   `json:"forecast"`
	Actual            float64   `json:"actual"`
	LastSent          time.Time `json:"last_sent"`
	LeaseUntil        time.Time `json:"lease_until"`
}

type budgetStateStore interface {
	Load(context.Context, string) (budgetState, int64, error)
	Save(context.Context, string, int64, budgetState) (int64, error)
}

type gcsBudgetStateStore struct {
	api    *storage.Service
	bucket string
}

func (g *gcsBudgetStateStore) Load(ctx context.Context, key string) (budgetState, int64, error) {
	var state budgetState
	obj, err := g.api.Objects.Get(g.bucket, key).Context(ctx).Do()
	if apiStatus(err, 404) {
		return state, 0, nil
	}
	if err != nil {
		return state, 0, err
	}
	resp, err := g.api.Objects.Get(g.bucket, key).Generation(obj.Generation).Context(ctx).Download()
	if err != nil {
		return state, 0, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&state)
	return state, obj.Generation, err
}

func (g *gcsBudgetStateStore) Save(ctx context.Context, key string, generation int64, state budgetState) (int64, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return 0, err
	}
	obj, err := g.api.Objects.Insert(g.bucket, &storage.Object{Name: key, ContentType: "application/json"}).
		Media(bytes.NewReader(data)).IfGenerationMatch(generation).Context(ctx).Do()
	if apiStatus(err, 412) {
		return 0, errStateConflict
	}
	if err != nil {
		return 0, err
	}
	return obj.Generation, nil
}

func apiStatus(err error, code int) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == code
}

func (b budgetNotification) thresholds() (float64, float64) {
	forecast := float64(b.ForecastThreshold)
	if forecast == 0 && b.ForecastThresholdAmount > 0 && b.BudgetAmount > 0 {
		forecast = float64(b.ForecastThresholdAmount / b.BudgetAmount)
	}
	return forecast, float64(b.AlertThresholdExceeded)
}

func (b budgetNotification) stateKey() (string, error) {
	if b.CostIntervalStart == "" || (b.BudgetID == "" && b.BudgetDisplayName == "") {
		return "", errors.New("budget state requires identity and billing interval")
	}
	interval, err := time.Parse(time.RFC3339, b.CostIntervalStart)
	if err != nil {
		return "", fmt.Errorf("invalid billing interval: %w", err)
	}
	identity := firstNonEmpty(b.BudgetID, b.BudgetDisplayName)
	data, _ := json.Marshal([]string{b.BillingAccountID, identity, interval.UTC().Format(time.RFC3339)})
	return fmt.Sprintf("budget/%x.json", sha256.Sum256(data)), nil
}

// Returns false only for a safely acknowledged, unchanged budget update.
// Store failures return errors so Pub/Sub retries instead of silently losing alerts.
func (s *server) deliverBudget(ctx context.Context, b budgetNotification) (bool, error) {
	forecast, actual := b.thresholds()
	if forecast <= 0 && actual <= 0 && b.PublishedAt.IsZero() {
		return false, nil
	}
	key, err := b.stateKey()
	if err != nil {
		return false, err
	}
	if s.budgetStore == nil {
		return false, errors.New("budget state store is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for attempt := 0; attempt < 5; attempt++ {
		state, generation, err := s.budgetStore.Load(ctx, key)
		if err != nil {
			return false, fmt.Errorf("load budget state: %w", err)
		}
		now := s.now().UTC()
		if state.LeaseUntil.After(now) {
			return false, errors.New("budget delivery in progress; retry")
		}
		// Pub/Sub publication order determines freshness, independently of the
		// historical thresholds already announced. Record even suppressed/clear
		// updates so a delayed old message cannot restore an obsolete warning.
		if !b.PublishedAt.IsZero() && !b.PublishedAt.After(state.LatestPublishedAt) {
			return false, nil
		}
		escalation := forecast > state.Forecast || actual > state.Actual
		aboveThreshold := forecast > 0 || actual > 0
		reminder := now.Sub(state.LastSent) >= 24*time.Hour
		if !aboveThreshold || (!state.LastSent.IsZero() && !escalation && !reminder) {
			if b.PublishedAt.IsZero() {
				return false, nil
			}
			state.LatestPublishedAt = b.PublishedAt
			_, err = s.budgetStore.Save(ctx, key, generation, state)
			if errors.Is(err, errStateConflict) {
				continue
			}
			return false, err
		}
		claimed := state
		claimed.LeaseUntil = now.Add(2 * time.Minute)
		generation, err = s.budgetStore.Save(ctx, key, generation, claimed)
		if errors.Is(err, errStateConflict) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("claim budget delivery: %w", err)
		}
		sendErr := s.postSlack(ctx, s.formatBudget(b))
		// Complete/release even when the incoming HTTP request has been cancelled.
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if sendErr == nil {
			state.LastSent = now
			if !b.PublishedAt.IsZero() {
				state.LatestPublishedAt = b.PublishedAt
			}
			state.Forecast = max(state.Forecast, forecast)
			state.Actual = max(state.Actual, actual)
		}
		state.LeaseUntil = time.Time{}
		_, saveErr := s.budgetStore.Save(finishCtx, key, generation, state)
		finishCancel()
		if sendErr != nil || saveErr != nil {
			return false, errors.Join(sendErr, saveErr)
		}
		return true, nil
	}
	return false, errStateConflict
}
