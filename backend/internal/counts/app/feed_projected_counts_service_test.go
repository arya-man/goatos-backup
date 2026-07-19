package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestProjectedShedCountsForFeedNormalizesQuery(t *testing.T) {
	t.Parallel()

	tenant := "00000000-0000-4000-8000-000000000001"
	park := "  00000000-0000-4000-8000-000000002001  "
	shed := "  00000000-0000-4000-8000-000000003001  "

	cases := []struct {
		name string
		in   domain.FeedProjectedCountQuery
		want func(t *testing.T, got domain.FeedProjectedCountQuery)
	}{
		{
			name: "target date is pinned to its India business day start",
			in: domain.FeedProjectedCountQuery{
				TenantID: tenant,
				// 20:00 UTC on the 10th is already 01:30 IST on the 11th. Passing this through
				// unnormalized would project the wrong feed day.
				TargetDate: time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC),
			},
			want: func(t *testing.T, got domain.FeedProjectedCountQuery) {
				want := time.Date(2026, time.July, 11, 0, 0, 0, 0, biztime.DefaultLocation())
				if !got.TargetDate.Equal(want) {
					t.Fatalf("TargetDate = %s, want %s", got.TargetDate, want)
				}
			},
		},
		{
			name: "a late same-day India instant stays on its own business day",
			in: domain.FeedProjectedCountQuery{
				TenantID:   tenant,
				TargetDate: time.Date(2026, time.July, 10, 23, 30, 0, 0, biztime.DefaultLocation()),
			},
			want: func(t *testing.T, got domain.FeedProjectedCountQuery) {
				want := time.Date(2026, time.July, 10, 0, 0, 0, 0, biztime.DefaultLocation())
				if !got.TargetDate.Equal(want) {
					t.Fatalf("TargetDate = %s, want %s", got.TargetDate, want)
				}
			},
		},
		{
			name: "filters are trimmed and lifecycle is lowercased",
			in: domain.FeedProjectedCountQuery{
				TenantID:        "  " + tenant + "  ",
				TargetDate:      time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation()),
				LifecycleStatus: strPtr("  ALIVE  "),
				ParkID:          strPtr(park),
				ShedID:          strPtr(shed),
			},
			want: func(t *testing.T, got domain.FeedProjectedCountQuery) {
				if got.TenantID != tenant {
					t.Fatalf("TenantID = %q, want %q", got.TenantID, tenant)
				}
				if got.LifecycleStatus == nil || *got.LifecycleStatus != "alive" {
					t.Fatalf("LifecycleStatus = %v, want alive", got.LifecycleStatus)
				}
				if got.ParkID == nil || *got.ParkID != "00000000-0000-4000-8000-000000002001" {
					t.Fatalf("ParkID = %v, want trimmed park", got.ParkID)
				}
				if got.ShedID == nil || *got.ShedID != "00000000-0000-4000-8000-000000003001" {
					t.Fatalf("ShedID = %v, want trimmed shed", got.ShedID)
				}
			},
		},
		{
			name: "an unset limit takes the default page size",
			in: domain.FeedProjectedCountQuery{
				TenantID:   tenant,
				TargetDate: time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation()),
			},
			want: func(t *testing.T, got domain.FeedProjectedCountQuery) {
				if got.Limit != defaultFeedProjectedCountLimit {
					t.Fatalf("Limit = %d, want %d", got.Limit, defaultFeedProjectedCountLimit)
				}
			},
		},
		{
			name: "an explicit limit at the maximum is preserved",
			in: domain.FeedProjectedCountQuery{
				TenantID:   tenant,
				TargetDate: time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation()),
				Limit:      maxFeedProjectedCountLimit,
			},
			want: func(t *testing.T, got domain.FeedProjectedCountQuery) {
				if got.Limit != maxFeedProjectedCountLimit {
					t.Fatalf("Limit = %d, want %d", got.Limit, maxFeedProjectedCountLimit)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeRepo{}
			svc := NewService(repo)
			if _, err := svc.ProjectedShedCountsForFeed(context.Background(), tc.in); err != nil {
				t.Fatalf("ProjectedShedCountsForFeed: %v", err)
			}
			tc.want(t, repo.feedProjectedQuery)
		})
	}
}

func TestProjectedShedCountsForFeedRejectsInvalidQuery(t *testing.T) {
	t.Parallel()

	tenant := "00000000-0000-4000-8000-000000000001"
	validDate := time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation())

	cases := []struct {
		name string
		in   domain.FeedProjectedCountQuery
		want error
	}{
		{
			name: "a blank tenant is rejected",
			in:   domain.FeedProjectedCountQuery{TenantID: "   ", TargetDate: validDate},
			want: ErrMissingRequiredField,
		},
		{
			// A zero target date must not silently become "today". A feed projection is only
			// meaningful for a day the caller named.
			name: "a zero target date is rejected rather than defaulted",
			in:   domain.FeedProjectedCountQuery{TenantID: tenant},
			want: ErrInvalidTargetDate,
		},
		{
			name: "a negative limit is rejected",
			in:   domain.FeedProjectedCountQuery{TenantID: tenant, TargetDate: validDate, Limit: -1},
			want: ErrInvalidLimit,
		},
		{
			name: "a limit past the maximum is rejected rather than clamped",
			in:   domain.FeedProjectedCountQuery{TenantID: tenant, TargetDate: validDate, Limit: maxFeedProjectedCountLimit + 1},
			want: ErrInvalidLimit,
		},
		{
			name: "a negative offset is rejected",
			in:   domain.FeedProjectedCountQuery{TenantID: tenant, TargetDate: validDate, Offset: -1},
			want: ErrInvalidOffset,
		},
		{
			// An unbounded OFFSET is the growable offset the scale rules ban; the service refuses
			// it rather than letting the query walk arbitrarily deep.
			name: "an offset past the bound is rejected",
			in:   domain.FeedProjectedCountQuery{TenantID: tenant, TargetDate: validDate, Offset: maxFeedProjectedCountOffset + 1},
			want: ErrInvalidOffset,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeRepo{}
			svc := NewService(repo)
			_, err := svc.ProjectedShedCountsForFeed(context.Background(), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if repo.feedProjectedQuery.TenantID != "" {
				t.Fatalf("repository was called for an invalid query: %+v", repo.feedProjectedQuery)
			}
		})
	}
}

func TestProjectedShedCountsForFeedSurfacesRepositoryError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	svc := NewService(&fakeRepo{feedProjectedErr: sentinel})

	_, err := svc.ProjectedShedCountsForFeed(context.Background(), domain.FeedProjectedCountQuery{
		TenantID:   "00000000-0000-4000-8000-000000000001",
		TargetDate: time.Date(2026, time.July, 10, 9, 0, 0, 0, biztime.DefaultLocation()),
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}
