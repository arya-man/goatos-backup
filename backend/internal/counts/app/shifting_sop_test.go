package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP (2026-09-16): a completion is judged against the card the movement was PINNED to at
// raise, an older app's legacy triple is accepted and mapped onto the seeded slots, and the
// verifier item names every capture by its slot title and carries every answer -- including the
// raise card's -- as grouped context rows. All on the production Complete path.

// sopShiftingRepo is a Repository + ShiftingSOPStore fake that records the completion command and
// echoes the SOP fields back as a store would (the row's STORED map feeds the enqueue).
type sopShiftingRepo struct {
	ports.Repository
	pin      ports.ShiftingSOPPin
	pinErr   error
	result   domain.ShiftingExecutionResult
	last     domain.ShiftingCompletionCommand
	calls    int
	page     domain.ShiftingExecutionPage
	replayed bool
}

func (r *sopShiftingRepo) ShiftingSOPPin(context.Context, string, string) (ports.ShiftingSOPPin, error) {
	return r.pin, r.pinErr
}

func (r *sopShiftingRepo) ShiftingEventByIdempotencyKey(context.Context, string, string, string) (string, bool, error) {
	return "", false, nil
}

func (r *sopShiftingRepo) CompleteShiftingEvent(_ context.Context, in domain.ShiftingCompletionCommand) (domain.ShiftingExecutionResult, bool, error) {
	r.calls++
	r.last = in
	out := r.result
	out.ShiftingEventID = in.ShiftingEventID
	out.SOPProofs = in.SOPProofs
	out.SOPAnswers = in.SOPAnswers
	out.Priority = r.pin.Priority
	out.SOPVersion = r.pin.Version
	return out, r.replayed, nil
}

func (r *sopShiftingRepo) ListShiftingEventsPendingExecution(context.Context, domain.ShiftingExecutionQuery) (domain.ShiftingExecutionPage, error) {
	return r.page, nil
}

type sopProofMedia struct {
	kinds map[string]string
	seen  []ports.ExpectedShiftingProofMedia
}

func (m *sopProofMedia) ValidateShiftingProofMedia(_ context.Context, _ string, expected []ports.ExpectedShiftingProofMedia) error {
	m.seen = append(m.seen, expected...)
	for _, e := range expected {
		got, ok := m.kinds[e.ProofID]
		if !ok {
			return e.OnAbsent
		}
		if e.Kind != authored.KindEither && got != e.Kind {
			return e.OnAbsent
		}
	}
	return nil
}

func (m *sopProofMedia) DescribeShiftingProofMedia(_ context.Context, _ string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if k, ok := m.kinds[id]; ok {
			out[id] = k
		}
	}
	return out, nil
}

func intp(v int) *int { return &v }

func ans(v string) json.RawMessage { raw, _ := json.Marshal(v); return raw }

// pinnedCard is version 1: the seeded completion slot plus a required pick-one, and a high card
// with one `either` slot instead of the two seeded clips.
func pinnedRules() domain.ShiftingRules {
	r := domain.SeededShiftingRules()
	r.Version = 1
	r.Completion.Questions = []authored.Question{{ID: "calm", Kind: authored.QuestionChoice, Title: "Animals calm?", Required: true,
		Options: []authored.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
	r.HighPriority.Proofs = []authored.ProofSlot{{Key: "feed_clip", Title: "Feed clip", Kind: authored.KindEither, Required: true}}
	r.Raise.Questions = []authored.Question{{ID: "why", Kind: authored.QuestionText, Title: "Why move", Required: true}}
	r.Raise.Proofs = []authored.ProofSlot{{Key: "pen_photo", Title: "Pen photo", Kind: authored.KindPhoto, Required: false}}
	return r
}

// laterRules is version 2, published AFTER the movement was raised: a renamed completion slot and
// a different required question. A completion pinned to v1 must never be judged by it.
func laterRules() domain.ShiftingRules {
	r := domain.SeededShiftingRules()
	r.Version = 2
	r.Completion.Proofs = []authored.ProofSlot{{Key: "walk_video", Title: "Walk video", Kind: authored.KindVideo, Required: true}}
	r.Completion.Questions = []authored.Question{{ID: "count_ok", Kind: authored.QuestionText, Title: "Count ok", Required: true}}
	return r
}

func sopRules() *ports.StaticShiftingSOPRules {
	return &ports.StaticShiftingSOPRules{Published: laterRules(), ByVersion: map[int]domain.ShiftingRules{1: pinnedRules(), 2: laterRules()}}
}

func sopService(t *testing.T, repo *sopShiftingRepo, rules ports.ShiftingSOPRulesSource, media ports.ShiftingProofMedia) (*ShiftingExecutionService, *capturingEnqueuer) {
	t.Helper()
	enq := &capturingEnqueuer{}
	svc := NewShiftingExecutionService(repo, func() time.Time { return time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) }).
		WithVerificationEnqueuer(enq)
	if rules != nil {
		svc = svc.WithSOPRules(rules, repo)
	} else {
		svc = svc.WithSOPRules(nil, repo)
	}
	if media != nil {
		svc = svc.WithProofMedia(media)
	}
	return svc, enq
}

func approvedPin(version *int, priority string) ports.ShiftingSOPPin {
	return ports.ShiftingSOPPin{Version: version, Priority: priority, AuthorizationState: "authorized", EventStatus: domain.ShiftingEventStatusAuthorized, VerificationState: "unverified"}
}

func lowResult() domain.ShiftingExecutionResult {
	return domain.ShiftingExecutionResult{EventStatus: domain.ShiftingEventStatusApplied, DestinationShedName: "Castro", DestinationPartitionLabel: "2",
		SourceShedName: "Castro", SourcePartitionLabel: "1", MovedGoatIDs: []string{"g1", "g2"}}
}

func baseInput() CompleteShiftingInput {
	return CompleteShiftingInput{TenantID: "t", ShiftingEventID: "ev1", CompletedByUserID: "op", IdempotencyKey: "k1", RequestFingerprint: "fp1"}
}

func TestCompleteJudgesAgainstPinnedVersionNotLatestPublish(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "low"), result: lowResult()}
	media := &sopProofMedia{kinds: map[string]string{"v1": "video"}}
	svc, enq := sopService(t, repo, sopRules(), media)
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1"}
	in.SOPAnswers = authored.Answers{"calm": ans("yes")}
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("Complete on the pinned v1 card: %v", err)
	}
	// Judged by v1 (seeded slot + "calm"), not by v2 (walk_video + count_ok).
	if repo.last.SOPProofs[domain.SlotShiftingVideo] != "v1" || repo.last.ProofRef != "v1" {
		t.Fatalf("stored proofs = %v proof_ref=%q", repo.last.SOPProofs, repo.last.ProofRef)
	}
	if len(enq.request.MediaMeta) != 1 || enq.request.MediaMeta[0].Label != "Shifting video" || enq.request.MediaMeta[0].Kind != "video" {
		t.Fatalf("media meta = %+v", enq.request.MediaMeta)
	}
	// The v2 shape is refused on the same pin.
	in2 := baseInput()
	in2.SOPProofs = authored.ProofRefs{"walk_video": "v1"}
	in2.SOPAnswers = authored.Answers{"count_ok": ans("12")}
	_, _, err := svc.Complete(context.Background(), in2)
	if key, _, ok := SOPProofSlotError(err); !ok || key != "walk_video" {
		t.Fatalf("v2 shape on a v1 pin: err=%v key=%q", err, key)
	}
}

func TestCompleteRefusesMissingUnknownDuplicateAndWrongKindSlotByName(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "high"), result: lowResult()}
	media := &sopProofMedia{kinds: map[string]string{"v1": "video", "p1": "photo", "f1": "video"}}
	svc, _ := sopService(t, repo, sopRules(), media)
	cases := []struct {
		name   string
		proofs authored.ProofRefs
		want   string
	}{
		{"missing compulsory", authored.ProofRefs{domain.SlotShiftingVideo: "v1"}, "feed_clip"},
		{"unknown slot", authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "f1", "bogus": "x"}, "bogus"},
		{"duplicate ref", authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "v1"}, "feed_clip"},
		{"wrong kind", authored.ProofRefs{domain.SlotShiftingVideo: "p1", "feed_clip": "f1"}, domain.SlotShiftingVideo},
	}
	for _, c := range cases {
		in := baseInput()
		in.SOPProofs = c.proofs
		in.SOPAnswers = authored.Answers{"calm": ans("yes")}
		_, _, err := svc.Complete(context.Background(), in)
		key, _, ok := SOPProofSlotError(err)
		if !ok || key != c.want {
			t.Fatalf("%s: err=%v key=%q want %q", c.name, err, key, c.want)
		}
		if repo.calls != 0 {
			t.Fatalf("%s: a refused completion must write nothing", c.name)
		}
	}
}

func TestCompleteRefusesUnansweredRequiredQuestion(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "low"), result: lowResult()}
	svc, _ := sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{"v1": "video"}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1"}
	_, _, err := svc.Complete(context.Background(), in)
	if id, _, ok := SOPAnswerError(err); !ok || id != "calm" {
		t.Fatalf("err=%v id=%q", err, id)
	}
	if repo.calls != 0 {
		t.Fatal("refused completion wrote")
	}
}

// An older app sends proof_ref + the two feed refs and nothing else. On the SEEDED card that is
// exactly the three seeded slots, and the legacy mirrors come back filled.
func TestLegacyTripleStillCompletesHighPriority(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(nil, "high"), result: lowResult()}
	media := &sopProofMedia{kinds: map[string]string{"v1": "video", "p1": "video", "f1": "video"}}
	svc, enq := sopService(t, repo, &ports.StaticShiftingSOPRules{}, media)
	in := baseInput()
	in.LegacyShape = true
	in.ProofRef, in.FeedPackingProofRef, in.FeedGivenProofRef, in.FeedConfigFingerprint = "v1", "p1", "f1", "fp"
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := authored.ProofRefs{domain.SlotShiftingVideo: "v1", domain.SlotShiftingPackingVideo: "p1", domain.SlotShiftingFeedingVideo: "f1"}
	if len(repo.last.SOPProofs) != 3 {
		t.Fatalf("stored = %v want %v", repo.last.SOPProofs, want)
	}
	for k, v := range want {
		if repo.last.SOPProofs[k] != v {
			t.Fatalf("stored[%s] = %q want %q", k, repo.last.SOPProofs[k], v)
		}
	}
	if repo.last.ProofRef != "v1" || repo.last.FeedPackingProofRef != "p1" || repo.last.FeedGivenProofRef != "f1" || repo.last.FeedConfigFingerprint != "fp" {
		t.Fatalf("mirrors = %q %q %q %q", repo.last.ProofRef, repo.last.FeedPackingProofRef, repo.last.FeedGivenProofRef, repo.last.FeedConfigFingerprint)
	}
	if got := strings.Join(enq.request.MediaRefs, ","); got != "v1,p1,f1" {
		t.Fatalf("media refs = %s", got)
	}
	labels := []string{}
	for _, m := range enq.request.MediaMeta {
		labels = append(labels, m.Label)
	}
	if got := strings.Join(labels, "|"); got != "Shifting video|Feed packing video|Feed given to animal video" {
		t.Fatalf("labels = %s", got)
	}
	if enq.request.IdempotencyKey != "counts-shifting-verification:ev1:v1:p1:f1" {
		t.Fatalf("legacy key changed: %s", enq.request.IdempotencyKey)
	}
	// No answers, no raise captures: no grouped rows beyond the two pens.
	if len(enq.request.ContextRows) != 2 {
		t.Fatalf("rows = %+v", enq.request.ContextRows)
	}
}

// An older app against an AUTHORED card: the request is accepted, what it sent lands on the seeded
// keys, and every compulsory slot / required question it could not send becomes a "Not captured
// (older app)" row for the verifier. A NEW app (proofs present) is judged strictly instead.
func TestOlderAppCompletionIsAcceptedWithNotCapturedRows(t *testing.T) {
	rules := pinnedRules()
	rules.Completion.Proofs = append(rules.Completion.Proofs, authored.ProofSlot{Key: "arrival_photo", Title: "Arrival photo", Kind: authored.KindPhoto, Required: true})
	src := &ports.StaticShiftingSOPRules{ByVersion: map[int]domain.ShiftingRules{1: rules}}
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "low"), result: lowResult()}
	svc, enq := sopService(t, repo, src, &sopProofMedia{kinds: map[string]string{"v1": "video"}})
	in := baseInput()
	in.LegacyShape = true
	in.ProofRef = "v1"
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("older app refused: %v", err)
	}
	if repo.last.SOPProofs[domain.SlotShiftingVideo] != "v1" || len(repo.last.SOPProofs) != 1 {
		t.Fatalf("stored = %v", repo.last.SOPProofs)
	}
	rows := map[string]VerificationContextRow{}
	for _, r := range enq.request.ContextRows {
		rows[r.Label] = r
	}
	if r := rows["Arrival photo"]; r.Value != "Not captured (older app)" || r.Group != "Completion" {
		t.Fatalf("arrival row = %+v (all %+v)", r, enq.request.ContextRows)
	}
	if r := rows["Animals calm?"]; r.Value != "Not captured (older app)" || r.Group != "Completion" {
		t.Fatalf("question row = %+v", r)
	}
	// The same body from a NEW app (proofs key present) is judged strictly.
	strict := baseInput()
	strict.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1"}
	strict.SOPAnswers = authored.Answers{"calm": ans("yes")}
	_, _, err := svc.Complete(context.Background(), strict)
	if key, _, ok := SOPProofSlotError(err); !ok || key != "arrival_photo" {
		t.Fatalf("new app not judged strictly: err=%v key=%q", err, key)
	}
}

func TestUnapprovedCompletionIsNotAuthorizedBeforeSlotJudgement(t *testing.T) {
	pin := approvedPin(intp(1), "high")
	pin.AuthorizationState, pin.EventStatus = "pending", domain.ShiftingEventStatusPending
	repo := &sopShiftingRepo{pin: pin, result: lowResult()}
	svc, _ := sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{} // every compulsory slot missing
	_, _, err := svc.Complete(context.Background(), in)
	if !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("err = %v, want not-authorized before any slot is judged", err)
	}
	if repo.calls != 0 {
		t.Fatal("wrote")
	}
}

func TestCompleteEnqueuesMediaMetaAndAnswerRowsIncludingRaiseCapture(t *testing.T) {
	result := lowResult()
	result.RaiseSOPProofs = authored.ProofRefs{"pen_photo": "r1"}
	result.RaiseSOPAnswers = authored.Answers{"why": ans("Overcrowded")}
	result.RaiseCaptureEvidence = json.RawMessage(`{"version_label":"SOP v1","rows":[{"label":"Why move","value":"Overcrowded","group":"At raise"}],"media":[{"proof_id":"r1","label":"Pen photo","kind":"photo"}]}`)
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "high"), result: result}
	media := &sopProofMedia{kinds: map[string]string{"v1": "video", "f1": "photo"}}
	svc, enq := sopService(t, repo, sopRules(), media)
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "f1"}
	in.SOPAnswers = authored.Answers{"calm": ans("no")}
	in.FeedConfigFingerprint = "fp"
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := strings.Join(enq.request.MediaRefs, ","); got != "v1,f1,r1" {
		t.Fatalf("media order = %s, want completion, high, raise", got)
	}
	meta := enq.request.MediaMeta
	if len(meta) != 3 || meta[0].Label != "Shifting video" || meta[1].Label != "Feed clip" || meta[1].Kind != "photo" ||
		meta[2].Label != "At raise · Pen photo" || meta[2].Kind != "photo" {
		t.Fatalf("meta = %+v", meta)
	}
	want := []VerificationContextRow{
		{Label: "Moved from", Value: "Castro 1"},
		{Label: "Moved to", Value: "Castro 2"},
		{Label: "Why move", Value: "Overcrowded", Group: "At raise"},
		{Label: "Animals calm?", Value: "No", Group: "Completion"},
	}
	if len(enq.request.ContextRows) != len(want) {
		t.Fatalf("rows = %+v", enq.request.ContextRows)
	}
	for i := range want {
		if enq.request.ContextRows[i] != want[i] {
			t.Fatalf("row %d = %+v want %+v", i, enq.request.ContextRows[i], want[i])
		}
	}
	// Answers + raise captures fold into the key, so this is not the legacy shape.
	if enq.request.IdempotencyKey == "counts-shifting-verification:ev1:v1:f1" || !strings.HasPrefix(enq.request.IdempotencyKey, "counts-shifting-verification:ev1:v1:f1:") {
		t.Fatalf("key = %s", enq.request.IdempotencyKey)
	}
	// The proof register was asked for exactly the completion captures, of the slot's kind.
	if len(media.seen) != 2 || media.seen[1].Kind != authored.KindEither {
		t.Fatalf("register asked = %+v", media.seen)
	}
}

func TestEitherSlotCapturedKindRidesMediaMeta(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "high"), result: lowResult()}
	svc, enq := sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{"v1": "video", "f1": "video"}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "f1"}
	in.SOPAnswers = authored.Answers{"calm": ans("yes")}
	in.FeedConfigFingerprint = "fp"
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if enq.request.MediaMeta[1].Kind != "video" {
		t.Fatalf("either slot kind = %q, want the register's video", enq.request.MediaMeta[1].Kind)
	}
	// The mirrors: no seeded feed slot on this card, so feed columns stay blank while proof_ref
	// mirrors the seeded shifting slot.
	if repo.last.ProofRef != "v1" || repo.last.FeedPackingProofRef != "" || repo.last.FeedGivenProofRef != "" {
		t.Fatalf("mirrors = %q %q %q", repo.last.ProofRef, repo.last.FeedPackingProofRef, repo.last.FeedGivenProofRef)
	}
}

func TestUnknownPinnedVersionRefusedByName(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(9), "low"), result: lowResult()}
	svc, _ := sopService(t, repo, sopRules(), nil)
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1"}
	_, _, err := svc.Complete(context.Background(), in)
	if !errors.Is(err, ports.ErrShiftingSOPVersionUnknown) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnwiredRulesSourceRunsSeed(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(5), "low"), result: lowResult()}
	svc, enq := sopService(t, repo, nil, nil)
	in := baseInput()
	in.ProofRef = "v1"
	in.LegacyShape = true
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("unwired source must run the seed: %v", err)
	}
	if enq.request.MediaMeta[0].Label != "Shifting video" {
		t.Fatalf("meta = %+v", enq.request.MediaMeta)
	}
}

func TestPendingExecutionReadsEachPinnedVersionOnce(t *testing.T) {
	page := domain.ShiftingExecutionPage{Items: []domain.ShiftingExecutionRow{
		{ShiftingEventID: "a", Priority: "low", SOPVersion: intp(1)},
		{ShiftingEventID: "b", Priority: "high", SOPVersion: intp(1)},
		{ShiftingEventID: "c", Priority: "high", SOPVersion: intp(2)},
		{ShiftingEventID: "d", Priority: "low"},
	}}
	repo := &sopShiftingRepo{page: page}
	src := sopRules()
	svc, _ := sopService(t, repo, src, nil)
	got, err := svc.ListPendingExecution(context.Background(), "t", "", "", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if src.Calls != 1 {
		t.Fatalf("rules read %d times for one page, want 1", src.Calls)
	}
	if got.Items[0].SOP == nil || got.Items[0].SOP.Version != 1 || got.Items[0].HighPrioritySOP != nil {
		t.Fatalf("row a = %+v", got.Items[0])
	}
	if got.Items[1].HighPrioritySOP == nil || got.Items[1].HighPrioritySOP.Proofs[0].Key != "feed_clip" {
		t.Fatalf("row b high card = %+v", got.Items[1].HighPrioritySOP)
	}
	if got.Items[2].SOP.Proofs[0].Key != "walk_video" {
		t.Fatalf("row c = %+v", got.Items[2].SOP)
	}
	if got.Items[3].SOP == nil || got.Items[3].SOP.Version != 0 || got.Items[3].SOP.Proofs[0].Key != domain.SlotShiftingVideo {
		t.Fatalf("row d (seeded) = %+v", got.Items[3].SOP)
	}
}

// DEPLOY-DAY PARITY (maintainer 2026-09-16): shifting has always accepted a rework resubmit that
// names the capture the verifier rejected (its same-refs key collapses onto the existing item), so
// the SOP card must not add a refusal of its own. Stored answers are still kept when the resubmit
// carries none.
func TestReworkAcceptsTheSameCaptureLikeTodayAndKeepsAnswers(t *testing.T) {
	pin := approvedPin(intp(1), "low")
	pin.EventStatus, pin.VerificationState = domain.ShiftingEventStatusApplied, "rejected"
	pin.StoredProofs = authored.ProofRefs{domain.SlotShiftingVideo: "old"}
	pin.StoredAnswers = authored.Answers{"calm": ans("yes")}
	repo := &sopShiftingRepo{pin: pin, result: lowResult()}
	svc, enq := sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{"old": "video", "new": "video"}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "old"}
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("resubmitting the rejected capture was refused; today it is accepted: %v", err)
	}
	if string(repo.last.SOPAnswers["calm"]) != `"yes"` {
		t.Fatalf("answers not kept: %v", repo.last.SOPAnswers)
	}
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "new"}
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("fresh capture refused: %v", err)
	}
	if enq.request.MediaRefs[0] != "new" {
		t.Fatalf("queued %v", enq.request.MediaRefs)
	}
}

// A NEW-shaped request naming a seeded key the pinned card authored away is refused BY NAME. The
// seeded-key re-targeting exists only for an older app's legacy fields; applying it to an explicit
// map would silently file a capture under a slot the operator never recorded it for.
func TestExplicitSeededKeyNotOnPinnedCardIsRefusedNotRetargeted(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(intp(1), "high"), result: lowResult()}
	svc, _ := sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{"v1": "video", "p1": "video"}})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{domain.SlotShiftingVideo: "v1", domain.SlotShiftingPackingVideo: "p1"}
	in.SOPAnswers = authored.Answers{"calm": ans("yes")}
	_, _, err := svc.Complete(context.Background(), in)
	if key, _, ok := SOPProofSlotError(err); !ok || key != domain.SlotShiftingPackingVideo {
		t.Fatalf("err=%v key=%q, want the authored-away seeded key refused by name", err, key)
	}
	if repo.calls != 0 {
		t.Fatal("a refused completion wrote")
	}
	// An OLDER app's legacy packing field on the same card still lands on the authored feed slot.
	// (The older app always sends BOTH feed clips on a high movement; the second has no free slot
	// on this card and is dropped.)
	svc, _ = sopService(t, repo, sopRules(), &sopProofMedia{kinds: map[string]string{"v1": "video", "p1": "video", "f1": "video"}})
	legacy := baseInput()
	legacy.LegacyShape = true
	legacy.ProofRef, legacy.FeedPackingProofRef, legacy.FeedGivenProofRef = "v1", "p1", "f1"
	if _, _, err := svc.Complete(context.Background(), legacy); err != nil {
		t.Fatalf("older app refused: %v", err)
	}
	if repo.last.SOPProofs["feed_clip"] != "p1" {
		t.Fatalf("legacy packing ref not re-targeted onto the authored slot: %v", repo.last.SOPProofs)
	}
}

// DEPLOY-DAY PARITY (E2E 2026-09-17): before the SOP card an older app's high-priority completion
// that omitted either feed clip was refused `feed_proofs_required` -- the old phone always had
// both to send, so the refusal forced no update. The lenient older-app mapping must not turn that
// into an accepted high-priority move with no feed evidence (live repro: proof_ref + fingerprint
// alone applied the move and queued "Not captured (older app)" rows). Nothing is written or queued.
func TestLegacyHighPriorityCompletionWithoutFeedClipsIsRefusedAsBefore(t *testing.T) {
	for name, set := range map[string]func(*CompleteShiftingInput){
		"no feed clips":   func(in *CompleteShiftingInput) {},
		"no feeding clip": func(in *CompleteShiftingInput) { in.FeedPackingProofRef = "p1" },
		"no packing clip": func(in *CompleteShiftingInput) { in.FeedGivenProofRef = "f1" },
	} {
		t.Run(name, func(t *testing.T) {
			for _, version := range []*int{nil, intp(1)} {
				repo := &sopShiftingRepo{pin: approvedPin(version, "high"), result: lowResult()}
				media := &sopProofMedia{kinds: map[string]string{"v1": "video", "p1": "video", "f1": "video"}}
				svc, enq := sopService(t, repo, &ports.StaticShiftingSOPRules{ByVersion: map[int]domain.ShiftingRules{1: pinnedRules()}}, media)
				in := baseInput()
				in.LegacyShape = true
				in.ProofRef, in.FeedConfigFingerprint = "v1", "fp"
				set(&in)
				_, _, err := svc.Complete(context.Background(), in)
				if !errors.Is(err, ports.ErrShiftingFeedProofsRequired) {
					t.Fatalf("version %v: err = %v, want ErrShiftingFeedProofsRequired", version, err)
				}
				if repo.calls != 0 || enq.request.ShiftingEventID != "" {
					t.Fatalf("version %v: wrote %d completions / queued %+v on a refused completion", version, repo.calls, enq.request)
				}
			}
		})
	}
	// A LOW movement from the same older app never needed feed clips.
	repo := &sopShiftingRepo{pin: approvedPin(nil, "low"), result: lowResult()}
	svc, _ := sopService(t, repo, &ports.StaticShiftingSOPRules{}, &sopProofMedia{kinds: map[string]string{"v1": "video"}})
	in := baseInput()
	in.LegacyShape = true
	in.ProofRef = "v1"
	if _, _, err := svc.Complete(context.Background(), in); err != nil {
		t.Fatalf("low legacy completion refused: %v", err)
	}
}

// E2E 2026-09-17: a NEW app's completion with an EMPTY `proofs` map answered the legacy
// `proof_required` ("a video proof (proof_ref) is required") -- a field the new app never sends --
// instead of naming the compulsory slot it must fill. The legacy answer belongs to the legacy shape.
func TestNewShapeCompletionWithNoCapturesIsRefusedByTheCompulsorySlot(t *testing.T) {
	repo := &sopShiftingRepo{pin: approvedPin(nil, "high"), result: lowResult()}
	svc, _ := sopService(t, repo, &ports.StaticShiftingSOPRules{}, &sopProofMedia{})
	in := baseInput()
	in.SOPProofs = authored.ProofRefs{}
	in.FeedConfigFingerprint = "fp"
	_, _, err := svc.Complete(context.Background(), in)
	if key, _, ok := SOPProofSlotError(err); !ok || key != domain.SlotShiftingVideo {
		t.Fatalf("err=%v key=%q, want the compulsory Shifting video slot named", err, key)
	}
	if repo.calls != 0 {
		t.Fatal("a refused completion wrote")
	}
	legacy := baseInput()
	legacy.LegacyShape = true
	if _, _, err := svc.Complete(context.Background(), legacy); !errors.Is(err, ports.ErrShiftingProofRequired) {
		t.Fatalf("legacy err=%v, want the pre-SOP proof_required", err)
	}
}
