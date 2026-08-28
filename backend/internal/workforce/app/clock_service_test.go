package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

type fakeClockRepo struct {
	lastPunch  *ports.ClockPunchCommand
	punchEntry ports.ClockEntryRow
	punchErr   error
	day        *ports.ClockEntryRow
	recent     []ports.ClockEntryRow
	page       ports.ClockPresencePage
}

func (f *fakeClockRepo) RecordClockPunch(_ context.Context, cmd ports.ClockPunchCommand) (ports.ClockPunchRecord, error) {
	f.lastPunch = &cmd
	if f.punchErr != nil {
		return ports.ClockPunchRecord{}, f.punchErr
	}
	return ports.ClockPunchRecord{Entry: f.punchEntry}, nil
}

func (f *fakeClockRepo) ClockDayForMember(context.Context, ports.ClockStatusParams) (*ports.ClockEntryRow, []ports.ClockEntryRow, error) {
	return f.day, f.recent, nil
}

func (f *fakeClockRepo) ListClockPresence(context.Context, ports.ClockPresenceParams) (ports.ClockPresencePage, error) {
	return f.page, nil
}

func (f *fakeClockRepo) ClockPersonDayDetail(context.Context, string, string, string) (ports.ClockPersonDay, error) {
	return ports.ClockPersonDay{}, ports.ErrNotFound
}

func (f *fakeClockRepo) ClockEntryDetail(context.Context, string, string) (ports.ClockPersonDay, error) {
	return ports.ClockPersonDay{}, ports.ErrNotFound
}

type fakeClockPeople struct{}

func (fakeClockPeople) ListPeople(context.Context, ports.ListPeopleParams) ([]domain.PersonSummary, string, error) {
	return nil, "", nil
}
func (fakeClockPeople) PeopleCatalog(context.Context, string) (domain.PeopleCatalog, error) {
	return domain.PeopleCatalog{Parks: []domain.PeopleCatalogOption{{ID: "p1", Code: "CPT", Label: "CPT"}}}, nil
}
func (fakeClockPeople) PreflightCreatePerson(context.Context, ports.PreflightCreatePersonCommand) (ports.PreflightCreatePersonResult, error) {
	return ports.PreflightCreatePersonResult{}, nil
}
func (fakeClockPeople) CreatePerson(context.Context, ports.CreatePersonCommand) (domain.PersonSummary, error) {
	return domain.PersonSummary{}, nil
}

type fakeClockMember struct{ missing bool }

func (f fakeClockMember) GetMemberForActor(context.Context, string, string) (domain.OperatorProfile, error) {
	if f.missing {
		return domain.OperatorProfile{}, ports.ErrNotFound
	}
	return domain.OperatorProfile{OperatorID: "member-1", DisplayName: "Amit Kumar"}, nil
}

func newClockServiceForTest(repo *fakeClockRepo) *ClockService {
	return NewClockService(repo, fakeClockPeople{}, fakeClockMember{})
}

// The integrity gate refuses a mock-admitting payload BEFORE any repository
// call — a tampered client that skips its own check still cannot punch.
func TestPunchRefusesMockLocationBeforeAnyWrite(t *testing.T) {
	repo := &fakeClockRepo{}
	svc := newClockServiceForTest(repo)

	cases := []domain.ClockIntegrity{
		{MockLocation: true},
		{MockLocation: false, MockProviderPackages: []string{"com.fake.gps"}},
	}
	for _, integrity := range cases {
		_, err := svc.Punch(context.Background(), "t1", "u1", "clock_in", domain.ClockPunchRequest{
			IdempotencyKey: "k1",
			Integrity:      integrity,
		}, httpmiddleware.ClientInfo{}, "en", "trace")
		if err == nil {
			t.Fatalf("mock payload %+v must be refused", integrity)
		}
		appErr, ok := err.(*Error)
		if !ok || appErr.Code != "mock_location_detected" || appErr.HTTPStatus != 422 {
			t.Fatalf("want 422 mock_location_detected, got %#v", err)
		}
		if repo.lastPunch != nil {
			t.Fatalf("repository must not be reached for a mock punch")
		}
	}
}

// D1: an offline punch anchors its business day and effective instant on the
// DEVICE tap time, not server arrival; an online punch anchors on server now.
func TestOfflinePunchAnchorsOnDeviceCapturedAt(t *testing.T) {
	repo := &fakeClockRepo{punchEntry: ports.ClockEntryRow{
		ClockEntryID: "e1", WorkforceMemberID: "member-1",
		BusinessDate: "2026-08-27", Status: "open", ClockInAt: time.Now().Add(-30 * time.Hour),
	}}
	svc := newClockServiceForTest(repo)

	captured := time.Now().Add(-30 * time.Hour)
	_, err := svc.Punch(context.Background(), "t1", "u1", "clock_in", domain.ClockPunchRequest{
		IdempotencyKey: "k1",
		CapturedAt:     captured.Format(time.RFC3339),
		Offline:        true,
		Location:       domain.ClockLocation{Status: "captured"},
	}, httpmiddleware.ClientInfo{DeviceModel: "SM-A15"}, "en", "trace")
	if err != nil {
		t.Fatalf("Punch() error=%v", err)
	}
	got := repo.lastPunch
	if got.NetworkType != "offline_queued" {
		t.Fatalf("offline punch NetworkType=%q want offline_queued", got.NetworkType)
	}
	if got.BusinessDate != biztime.BusinessDate(captured) {
		t.Fatalf("offline punch BusinessDate=%q want the device day %q", got.BusinessDate, biztime.BusinessDate(captured))
	}
	if !got.EffectiveAt.Equal(got.CapturedAt) {
		t.Fatalf("offline punch must pair on captured_at")
	}
	if got.ClockSkewMs < 29*3600*1000 {
		t.Fatalf("skew must record the drain delay; got %dms", got.ClockSkewMs)
	}
	if got.DeviceModel != "SM-A15" {
		t.Fatalf("device snapshot must come from headers; got %q", got.DeviceModel)
	}
}

// Punch-order and duplicate refusals surface as farm-worded 409s.
func TestPunchMapsRepositoryRefusals(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ports.ErrAlreadyClockedIn, "already_clocked_in"},
		{ports.ErrNotClockedIn, "not_clocked_in"},
		{ports.ErrAlreadyClockedOut, "already_clocked_out"},
		{ports.ErrIdempotencyConflict, "idempotency_conflict"},
	}
	for _, tc := range cases {
		repo := &fakeClockRepo{punchErr: tc.err}
		svc := newClockServiceForTest(repo)
		_, err := svc.Punch(context.Background(), "t1", "u1", "clock_out", domain.ClockPunchRequest{
			IdempotencyKey: "k1",
			Location:       domain.ClockLocation{Status: "captured"},
		}, httpmiddleware.ClientInfo{}, "en", "trace")
		appErr, ok := err.(*Error)
		if !ok || appErr.Code != tc.code {
			t.Fatalf("repo err %v: want code %q got %#v", tc.err, tc.code, err)
		}
		if appErr.HTTPStatus != 409 {
			t.Fatalf("%s must be a 409; got %d", tc.code, appErr.HTTPStatus)
		}
		if appErr.Message == "" {
			t.Fatalf("%s must carry farm-worded copy", tc.code)
		}
	}
}

// The banner shows exactly while today has no entry, and clears on clock-in.
func TestStatusBannerShowsOnlyBeforeClockIn(t *testing.T) {
	repo := &fakeClockRepo{}
	svc := newClockServiceForTest(repo)
	status, err := svc.Status(context.Background(), "t1", "u1", "en", "trace")
	if err != nil {
		t.Fatalf("Status() error=%v", err)
	}
	if status.State != "not_clocked_in" || status.BannerText == "" {
		t.Fatalf("no entry: want not_clocked_in + banner, got state=%q banner=%q", status.State, status.BannerText)
	}

	repo.day = &ports.ClockEntryRow{
		ClockEntryID: "e1", WorkforceMemberID: "member-1",
		BusinessDate: biztime.BusinessDate(time.Now()), Status: "open", ClockInAt: time.Now().Add(-90 * time.Minute),
	}
	status, err = svc.Status(context.Background(), "t1", "u1", "en", "trace")
	if err != nil {
		t.Fatalf("Status() error=%v", err)
	}
	if status.State != "clocked_in" || status.BannerText != "" {
		t.Fatalf("open entry: want clocked_in + no banner, got state=%q banner=%q", status.State, status.BannerText)
	}
	if status.Entry == nil || !strings.Contains(status.Entry.HoursLabel, "1h 30m") {
		t.Fatalf("open today's entry must carry the live elapsed label; got %+v", status.Entry)
	}
}

// Hours truth stays backend-owned: a closed row renders its stored minutes,
// and an auto-closed / stale-open day renders NO hours and the honest flag.
func TestComposeEntryHoursAndFlags(t *testing.T) {
	svc := newClockServiceForTest(&fakeClockRepo{})
	copyMap := clockCopyFor("en")

	worked := 9*60 + 29
	out := time.Date(2026, 8, 27, 12, 1, 0, 0, time.UTC)
	closed := svc.composeEntry(ports.ClockEntryRow{
		ClockEntryID: "e1", BusinessDate: "2026-08-27", Status: "closed",
		ClockInAt: out.Add(-9*time.Hour - 29*time.Minute), ClockOutAt: &out, WorkedMinutes: &worked,
		OfflinePunch: true,
	}, "Amit", "", "", nil, nil, copyMap)
	if closed.HoursLabel != "9h 29m" {
		t.Fatalf("closed hours label=%q want 9h 29m", closed.HoursLabel)
	}
	if len(closed.Flags) != 1 || closed.Flags[0].Key != "offline" {
		t.Fatalf("offline punch must flag; got %+v", closed.Flags)
	}

	stale := svc.composeEntry(ports.ClockEntryRow{
		ClockEntryID: "e2", BusinessDate: "2020-01-01", Status: "open",
		ClockInAt: time.Date(2020, 1, 1, 3, 0, 0, 0, time.UTC), LocationMissing: true,
	}, "Amit", "", "", nil, nil, copyMap)
	if stale.HoursLabel != "" || stale.WorkedMinutes != nil {
		t.Fatalf("a stale open day must never invent hours; got %q", stale.HoursLabel)
	}
	keys := map[string]bool{}
	for _, f := range stale.Flags {
		keys[f.Key] = true
	}
	if !keys["not_clocked_out"] || !keys["no_location"] {
		t.Fatalf("stale open day must flag not_clocked_out + no_location; got %+v", stale.Flags)
	}
}

// Presence rows bucket working / clocked_out / not_clocked_in, and the row
// line is backend-composed.
func TestPresenceComposesBucketsAndRowLines(t *testing.T) {
	now := time.Now()
	today := biztime.BusinessDate(now)
	repo := &fakeClockRepo{page: ports.ClockPresencePage{
		Summary: domain.ClockPresenceSummary{Working: 1, ClockedOut: 1, NotClockedIn: 1},
		Rows: []ports.ClockPresenceRawRow{
			{WorkforceMemberID: "m1", PersonName: "Amit", RoleHint: "operator", ParkLabel: "CPT",
				Entry: &ports.ClockEntryRow{ClockEntryID: "e1", BusinessDate: today, Status: "open", ClockInAt: now.Add(-time.Hour)}},
			{WorkforceMemberID: "m2", PersonName: "Darshan", RoleHint: "operator",
				Entry: func() *ports.ClockEntryRow {
					worked := 545
					out := now
					return &ports.ClockEntryRow{ClockEntryID: "e2", BusinessDate: today, Status: "closed",
						ClockInAt: now.Add(-9 * time.Hour), ClockOutAt: &out, WorkedMinutes: &worked}
				}()},
			{WorkforceMemberID: "m3", PersonName: "Sagar", RoleHint: "operator"},
		},
	}}
	svc := newClockServiceForTest(repo)
	resp, err := svc.Presence(context.Background(), "t1", ports.ClockPresenceParams{}, "en", "trace")
	if err != nil {
		t.Fatalf("Presence() error=%v", err)
	}
	if !resp.IsToday || resp.BusinessDate != today {
		t.Fatalf("absent date must default to today; got %+v", resp.BusinessDate)
	}
	if len(resp.Rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(resp.Rows))
	}
	buckets := []string{resp.Rows[0].Bucket, resp.Rows[1].Bucket, resp.Rows[2].Bucket}
	want := []string{"working", "clocked_out", "not_clocked_in"}
	for i := range want {
		if buckets[i] != want[i] {
			t.Fatalf("bucket[%d]=%q want %q", i, buckets[i], want[i])
		}
	}
	if !strings.HasPrefix(resp.Rows[0].TimeLabel, "In ") || !strings.Contains(resp.Rows[0].TimeLabel, "so far") {
		t.Fatalf("working row line must be backend-composed; got %q", resp.Rows[0].TimeLabel)
	}
	if !strings.Contains(resp.Rows[1].TimeLabel, "9h 05m") {
		t.Fatalf("closed row line must carry the stored hours; got %q", resp.Rows[1].TimeLabel)
	}
	if resp.Rows[2].TimeLabel != "" {
		t.Fatalf("not-clocked-in row has no line; got %q", resp.Rows[2].TimeLabel)
	}
	if len(resp.Parks) != 1 || resp.Parks[0].Label != "CPT" {
		t.Fatalf("park filter options must come from the catalog; got %+v", resp.Parks)
	}
}

// Every locale's copy catalog carries the same keys as English — a missing key
// renders an empty label on a real phone with nothing failing anywhere.
func TestClockCopyCatalogsAgreeOnKeys(t *testing.T) {
	for _, tag := range []string{"hi", "kn", "te"} {
		other := clockCopyFor(tag)
		if len(other) != len(clockCopyEN) {
			t.Fatalf("%s copy catalog has %d keys, en has %d", tag, len(other), len(clockCopyEN))
		}
		for key := range clockCopyEN {
			if strings.TrimSpace(other[key]) == "" {
				t.Fatalf("%s copy catalog is missing %q", tag, key)
			}
		}
	}
}
