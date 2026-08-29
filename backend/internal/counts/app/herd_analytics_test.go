package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// A repo with no herd-analytics capability must fail CLOSED. An empty payload would
// render as a farm holding no animals at all, which is a different claim from "this
// read is not wired here" -- and the second one is the truth.
func TestGetHerdAnalyticsFailsClosedWithoutAReader(t *testing.T) {
	svc := NewHerdRegisterService(&fakeRepo{})
	if _, err := svc.GetHerdAnalytics(context.Background(), domain.HerdAnalyticsQuery{TenantID: "t"}); !errors.Is(err, ErrHerdAnalyticsUnavailable) {
		t.Fatalf("err=%v, want ErrHerdAnalyticsUnavailable", err)
	}
}
