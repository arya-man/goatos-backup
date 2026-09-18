package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
)

type fakeRepo struct {
	listParams ports.ListParams
	readParams ports.MarkReadParams
	page       domain.Page
	readCount  int
	err        error
}

func (f *fakeRepo) ListNotifications(_ context.Context, p ports.ListParams) (domain.Page, error) {
	f.listParams = p
	return f.page, f.err
}

func (f *fakeRepo) MarkRead(_ context.Context, p ports.MarkReadParams) (int, error) {
	f.readParams = p
	return f.readCount, f.err
}

// TestClampLimitRefusesOutOfRangePageSizes: absent means 20, and anything outside 1..50 is
// refused rather than clamped -- a caller who asked for 200 and silently got 20 would page
// past the 180 rows it never saw.
func TestClampLimitRefusesOutOfRangePageSizes(t *testing.T) {
	if got, err := domain.ClampLimit(0); got != domain.DefaultLimit || err != nil {
		t.Errorf("absent limit = %d, %v; want %d, nil", got, err, domain.DefaultLimit)
	}
	for _, ok := range []int{1, 20, 50} {
		if got, err := domain.ClampLimit(ok); got != ok || err != nil {
			t.Errorf("limit %d = %d, %v", ok, got, err)
		}
	}
	for _, bad := range []int{-1, 51, 200, 1000} {
		if _, err := domain.ClampLimit(bad); !errors.Is(err, domain.ErrInvalidLimit) {
			t.Errorf("limit %d must be refused, got %v", bad, err)
		}
	}
}

// TestListPassesOnlyTheCallersOwnIdentity: the service must forward the authenticated actor
// as the ONLY identity in the params. There is no field on ListParams that names anybody
// else, and this test is what notices if one is ever added and wired from the request.
func TestListPassesOnlyTheCallersOwnIdentity(t *testing.T) {
	repo := &fakeRepo{page: domain.Page{UnreadCount: 4}}
	svc := NewService(repo)
	page, err := svc.List(context.Background(), ListRequest{
		TenantID: " 00000000-0000-4000-8000-0000000000t1 ",
		ActorID:  " 00000000-0000-4000-8000-0000000000u1 ",
		Cursor:   " abc ",
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.UnreadCount != 4 {
		t.Errorf("unread = %d", page.UnreadCount)
	}
	if repo.listParams.MemberOrUserID != "00000000-0000-4000-8000-0000000000u1" {
		t.Errorf("member = %q; the feed must be scoped to the authenticated actor and nobody else", repo.listParams.MemberOrUserID)
	}
	if repo.listParams.TenantID != "00000000-0000-4000-8000-0000000000t1" || repo.listParams.Cursor != "abc" || repo.listParams.Limit != 5 {
		t.Errorf("params = %+v", repo.listParams)
	}
}

func TestMarkReadValidatesTheBatchShape(t *testing.T) {
	const good = "11111111-1111-4111-8111-111111111111"
	cases := []struct {
		name string
		req  MarkReadRequest
		want error
	}{
		{"no idempotency key", MarkReadRequest{IDs: []string{good}}, domain.ErrIdempotencyKeyRequired},
		{"empty id list", MarkReadRequest{IdempotencyKey: "k", IDs: nil}, domain.ErrNoIDs},
		{"malformed id", MarkReadRequest{IdempotencyKey: "k", IDs: []string{"not-a-uuid"}}, domain.ErrInvalidID},
		{"over the batch cap", MarkReadRequest{IdempotencyKey: "k", IDs: tooManyIDs()}, domain.ErrTooManyIDs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			if _, err := NewService(repo).MarkRead(context.Background(), c.req); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if repo.readParams.IdempotencyKey != "" || len(repo.readParams.IDs) != 0 {
				t.Error("storage must not be reached with a malformed batch")
			}
		})
	}
}

// A duplicated id is a client retry artefact, not an error, and must not be counted twice.
func TestMarkReadDedupesTheBatch(t *testing.T) {
	const a = "11111111-1111-4111-8111-111111111111"
	const b = "22222222-2222-4222-8222-222222222222"
	repo := &fakeRepo{readCount: 2}
	got, err := NewService(repo).MarkRead(context.Background(), MarkReadRequest{
		TenantID: "t", ActorID: "u", IdempotencyKey: "key",
		IDs: []string{a, b, a, " " + b + " "},
	})
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if got != 2 {
		t.Errorf("read count = %d", got)
	}
	if len(repo.readParams.IDs) != 2 {
		t.Fatalf("ids = %v; duplicates must collapse", repo.readParams.IDs)
	}
	if repo.readParams.MemberOrUserID != "u" {
		t.Errorf("mark-as-read must be scoped to the authenticated actor, got %q", repo.readParams.MemberOrUserID)
	}
}

// Every error this module can produce must map to a stable code and a farm-worded message,
// and an unknown error must never leak storage detail onto a screen.
func TestHTTPErrorMapsEveryKnownCause(t *testing.T) {
	cases := map[error]struct {
		code   string
		status int
	}{
		domain.ErrInvalidLimit:           {"invalid_limit", 400},
		domain.ErrInvalidCursor:          {"invalid_cursor", 400},
		ports.ErrInvalidArgument:         {"invalid_cursor", 400},
		domain.ErrNoIDs:                  {"no_notifications", 400},
		domain.ErrTooManyIDs:             {"too_many_notifications", 400},
		domain.ErrInvalidID:              {"invalid_notification_id", 400},
		domain.ErrIdempotencyKeyRequired: {"missing_idempotency_key", 400},
		ports.ErrIdempotencyConflict:     {"idempotency_conflict", 409},
	}
	for cause, want := range cases {
		got := HTTPError(cause)
		if got.Code != want.code || got.HTTPStatus != want.status {
			t.Errorf("%v -> %s/%d, want %s/%d", cause, got.Code, got.HTTPStatus, want.code, want.status)
		}
	}
	if HTTPError(nil) != nil {
		t.Error("a nil cause must map to no error")
	}
	unknown := HTTPError(errors.New("pgx: connection refused to 10.0.0.4:5432"))
	if unknown.HTTPStatus != 500 || unknown.Code != "internal_error" {
		t.Errorf("unknown = %s/%d", unknown.Code, unknown.HTTPStatus)
	}
	if strings.Contains(unknown.Message, "pgx") || strings.Contains(unknown.Message, "10.0.0.4") {
		t.Errorf("the 500 message leaks storage detail: %q", unknown.Message)
	}
}

func tooManyIDs() []string {
	out := make([]string, 0, domain.MaxMarkReadIDs+1)
	for i := 0; i <= domain.MaxMarkReadIDs; i++ {
		out = append(out, "not-a-uuid")
	}
	return out
}
