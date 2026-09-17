package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// kindRegister is a MediaResolver that also answers ProofMediaKinds. Every call is recorded so a
// test can prove the queue asked ONCE per page, only for refs lacking a kind, and never signed.
type kindRegister struct {
	mimes        map[string]string
	err          error
	calls        [][]string
	resolveCalls int
	ensureCalls  int
}

func (k *kindRegister) ResolveMedia(_ context.Context, _ string, ids []string) ([]domain.MediaItem, error) {
	k.resolveCalls++
	return nil, errors.New("queue must not sign")
}

func (k *kindRegister) EnsureEvidenceAvailable(context.Context, string, []string) error {
	k.ensureCalls++
	return nil
}

func (k *kindRegister) ProofMediaKinds(_ context.Context, _ string, ids []string) (map[string]string, error) {
	k.calls = append(k.calls, append([]string(nil), ids...))
	if k.err != nil {
		return nil, k.err
	}
	out := map[string]string{}
	for _, id := range ids {
		if m, ok := k.mimes[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

var _ ports.ProofMediaKindReader = (*kindRegister)(nil)

func kindService(t *testing.T, register *kindRegister) *Service {
	t.Helper()
	svc := NewService(newFakeRepo(), register)
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "feed", Module: "feed", Category: "feed_distribution",
		ExpectedMedia: []string{"photo", "video"}, MediaLabels: []string{"Feed weight photo", "Feed distribution video"},
	}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestResolveMediaReadsUnknownKindFromProofRegister(t *testing.T) {
	register := &kindRegister{mimes: map[string]string{"p2": "image/jpeg"}}
	svc := kindService(t, register)
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{{
		Category:  "feed_distribution",
		MediaRefs: []string{"p1", "p2"},
		MediaMeta: []domain.MediaMeta{{Label: "Feed video", Kind: "video"}, {Label: "Water proof", Kind: "either"}},
	}})
	media := rows[0].Media
	if media[0].MimeType != "video/mp4" || media[1].MimeType != "image/jpeg" || media[1].Label != "Water proof" {
		t.Fatalf("media = %+v, want the either slot played as the photo the register holds", media)
	}
}

// An item whose producer named its proofs owns its positions: the category's positional copy was
// written for a different card, so a slot neither side can type stays unknown rather than borrowing
// "video" from position 2 of the registry.
func TestResolveMediaAuthoredItemNeverTakesRegistryPositionalMime(t *testing.T) {
	register := &kindRegister{}
	svc := kindService(t, register)
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{{
		Category:  "feed_distribution",
		MediaRefs: []string{"p1", "p2"},
		MediaMeta: []domain.MediaMeta{{Label: "Bag", Kind: "photo"}, {Label: "Water", Kind: ""}},
	}})
	if got := rows[0].Media[1].MimeType; got != "" {
		t.Fatalf("authored unknown slot mime = %q, want blank (never the registry's positional video)", got)
	}
}

func TestResolveMediaBeyondExpectedMediaResolvesFromRegister(t *testing.T) {
	register := &kindRegister{mimes: map[string]string{"p3": "image/png"}}
	svc := kindService(t, register)
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{{
		Category: "feed_distribution", MediaRefs: []string{"p1", "p2", "p3"},
	}})
	if got := rows[0].Media[2].MimeType; got != "image/png" {
		t.Fatalf("third proof (beyond the category's two) mime = %q, want the register's image/png", got)
	}
}

func TestResolveMediaLegacyItemKeepsRegistryCopyWhenRegisterUnavailable(t *testing.T) {
	register := &kindRegister{err: errors.New("register down")}
	svc := kindService(t, register)
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{{
		Category: "feed_distribution", MediaRefs: []string{"p1", "p2"},
	}})
	media := rows[0].Media
	if media[0].MimeType != "image/jpeg" || media[1].MimeType != "video/mp4" {
		t.Fatalf("legacy media = %+v, want the registry's positional kinds", media)
	}
	if media[0].Label != "Feed weight photo" || media[1].Label != "Feed distribution video" {
		t.Fatalf("legacy labels = %+v", media)
	}
	if !rows[0].EvidenceLinkResolved {
		t.Fatal("a register failure must not mark evidence unavailable")
	}
}

func TestResolveMediaReadsRegisterOncePerPageOnlyForUnknownRefs(t *testing.T) {
	register := &kindRegister{}
	svc := kindService(t, register)
	svc.resolveMedia(context.Background(), testTenant, []domain.Item{
		{Category: "feed_distribution", MediaRefs: []string{"a1", "a2"}, MediaMeta: []domain.MediaMeta{{Label: "x", Kind: "video"}, {Label: "y", Kind: "either"}}},
		{Category: "feed_distribution", MediaRefs: []string{"b1"}},
		{Category: "feed_distribution", MediaRefs: []string{"c1"}, MediaMeta: []domain.MediaMeta{{Label: "z", Kind: "photo"}}},
	})
	if len(register.calls) != 1 {
		t.Fatalf("register calls = %d, want ONE per page", len(register.calls))
	}
	got := append([]string(nil), register.calls[0]...)
	sort.Strings(got)
	if strings.Join(got, ",") != "a2,b1" {
		t.Fatalf("register asked for %v, want only the refs lacking a kind [a2 b1]", got)
	}
	// A page where every proof is typed asks nothing.
	svc.resolveMedia(context.Background(), testTenant, []domain.Item{
		{Category: "feed_distribution", MediaRefs: []string{"c1"}, MediaMeta: []domain.MediaMeta{{Label: "z", Kind: "photo"}}},
	})
	if len(register.calls) != 1 {
		t.Fatalf("fully typed page read the register: %v", register.calls)
	}
}

func TestResolveMediaKindFallbackNeverSignsOrChangesEvidenceFlag(t *testing.T) {
	for _, register := range []*kindRegister{{mimes: map[string]string{"p1": "video/mp4"}}, {err: errors.New("down")}} {
		svc := kindService(t, register)
		rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{{
			Category: "feed_distribution", MediaRefs: []string{"p1"}, MediaMeta: []domain.MediaMeta{{Label: "Clip", Kind: ""}},
		}})
		if register.resolveCalls != 0 || register.ensureCalls != 0 {
			t.Fatalf("queue signed (%d) or statted (%d) proofs", register.resolveCalls, register.ensureCalls)
		}
		if len(rows[0].Media) != 1 || rows[0].Media[0].DownloadURL != "/app/proofs/p1/download" || !rows[0].EvidenceLinkResolved {
			t.Fatalf("row = %+v, want the stable route and evidence flag untouched", rows[0])
		}
	}
}

func TestListQueueKindFallbackReadsOnlyTheSampledPage(t *testing.T) {
	register := &kindRegister{}
	svc := kindService(t, register)
	// Anchored to TODAY's IST business day, never "now minus an hour": between 00:00 and 01:00 IST
	// an hour-anchored fixture lands on yesterday and the queue's business-day read drops it.
	base := biztime.BusinessDayStart(time.Now())
	for i := 0; i < 3; i++ {
		if _, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "feed", Module: "feed", Category: "feed_distribution",
			Source:    domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: fmt.Sprintf("c%d", i)},
			MediaRefs: []string{fmt.Sprintf("ref-%d", i)}, IdempotencyKey: fmt.Sprintf("k%d", i), CapturedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || len(register.calls) != 1 {
		t.Fatalf("items=%d calls=%v", len(result.Items), register.calls)
	}
	want := map[string]bool{}
	for _, row := range result.Items {
		want[row.Item.MediaRefs[0]] = true
	}
	if len(register.calls[0]) != 2 {
		t.Fatalf("register asked %v, want only the two served rows' refs", register.calls[0])
	}
	for _, id := range register.calls[0] {
		if !want[id] {
			t.Fatalf("register asked for %q, which is the probe row beyond the page", id)
		}
	}
}

func TestListQueueNumbersRepeatedProducerLabels(t *testing.T) {
	svc := kindService(t, &kindRegister{})
	if _, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "feed", Module: "feed", Category: "feed_distribution",
		Source:    domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: "c1"},
		MediaRefs: []string{"p1", "p2", "p3"}, IdempotencyKey: "k1", CapturedAt: time.Now(),
		MediaMeta: []domain.MediaMeta{{Label: "Iodine dipping", Kind: "video"}, {Label: "Iodine dipping", Kind: "photo"}, {Label: "", Kind: "video"}},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, m := range result.Items[0].Media {
		labels = append(labels, m.Label)
	}
	// A video and a photo under one title are told apart by kind, not numbered as one series.
	if strings.Join(labels, "|") != "Iodine dipping · video|Iodine dipping · photo|Proof 3" {
		t.Fatalf("labels = %q", labels)
	}
}

// A producer that names more proofs than it sends is a producer bug, but refusing the item blocked
// the enqueue -- and every recovery loop that retries it -- so the work never reached a verifier
// (2026-09-17 hardening). The item is created and the extra meta is dropped.
func TestCreateItemTruncatesMediaMetaLongerThanRefs(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &kindRegister{})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "feed", Module: "feed", Category: "feed_distribution"})
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "feed", Module: "feed", Category: "feed_distribution",
		Source:    domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: "c1"},
		MediaRefs: []string{"p1"}, IdempotencyKey: "k1",
		MediaMeta: []domain.MediaMeta{{Label: "a", Kind: "video"}, {Label: "b", Kind: "photo"}},
	})
	if err != nil {
		t.Fatalf("CreateItem refused an item whose media_meta outnumbers its refs: %v", err)
	}
	got := repo.items[result.Item.ItemID].MediaMeta
	if len(got) != 1 || got[0] != (domain.MediaMeta{Label: "a", Kind: "video"}) {
		t.Fatalf("stored meta = %+v, want truncated to the one ref", got)
	}
}

func TestCreateItemNormalizesMediaMetaKinds(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &kindRegister{})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "feed", Module: "feed", Category: "feed_distribution"})
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "feed", Module: "feed", Category: "feed_distribution",
		Source:    domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: "c1"},
		MediaRefs: []string{"p1", "p2", "p3"}, IdempotencyKey: "k1",
		MediaMeta: []domain.MediaMeta{{Label: " Bag ", Kind: "IMAGE"}, {Label: "Clip", Kind: "either"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := repo.items[result.Item.ItemID].MediaMeta
	if len(got) != 2 || got[0] != (domain.MediaMeta{Label: "Bag", Kind: "photo"}) || got[1] != (domain.MediaMeta{Label: "Clip", Kind: ""}) {
		t.Fatalf("stored meta = %+v", got)
	}
}
