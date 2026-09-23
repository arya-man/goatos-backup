package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

const mediaTestTenant = "11000000-0000-4000-8000-000000000001"

// stubService serves ONE task detail; the media seam is what this file is about.
type stubService struct{ row ports.TaskRow }

func (s stubService) ListTasks(context.Context, string, []string, int, string) (ports.TaskPage, error) {
	return ports.TaskPage{}, nil
}
func (s stubService) GetTask(context.Context, string, string) (ports.TaskRow, error) {
	return s.row, nil
}
func (s stubService) CompleteStep(context.Context, ports.CompleteStepParams) (ports.TaskRow, error) {
	return s.row, nil
}
func (s stubService) SubmitReading(context.Context, ports.SubmitParams) (ports.TaskRow, error) {
	return s.row, nil
}
func (s stubService) RecordVerdict(context.Context, ports.VerdictParams) (ports.TaskRow, error) {
	return s.row, nil
}
func (s stubService) Now() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }

// stubMedia resolves every ref except the ones named in `broken`.
type stubMedia struct {
	broken map[string]bool
	calls  int
}

func (m *stubMedia) ResolveProofMedia(_ context.Context, tenantID, ref string) (string, string, error) {
	m.calls++
	if m.broken[ref] {
		return "", "", errors.New("object missing")
	}
	if ref == "photo-7" {
		return "https://storage.example/" + ref + "?sig=x", "image/jpeg", nil
	}
	return "https://storage.example/" + ref + "?sig=x", "video/mp4", nil
}

func reviewedRound() ports.TaskRow {
	row := openRound()
	row.Task.Status = domain.StatusPendingReview
	row.Task.StripPhotoRef = "photo-7"
	row.Task.Outcome = domain.OutcomeNegative
	at := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for _, no := range []int{1, 2, 3} {
		row.Completions = append(row.Completions, domain.StepCompletion{
			StepNo:      no,
			ProofRef:    fmt.Sprintf("video-%d", no),
			CompletedBy: "Dinakar",
			CompletedAt: at,
		})
	}
	return row
}

func getDetail(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, h)
	req := httptest.NewRequest(http.MethodGet, "/app/toxin/tasks/"+reviewedRound().Task.TaskID, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), mediaTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("detail body is not JSON: %v", err)
	}
	return body
}

func doneStepMedia(t *testing.T, body map[string]any) map[int]map[string]any {
	t.Helper()
	out := map[int]map[string]any{}
	steps, _ := body["steps"].([]any)
	for _, raw := range steps {
		step, _ := raw.(map[string]any)
		// The settling WAIT row also reports done once its hour has passed; it carries no
		// proof of its own, so it is not evidence and is not counted here.
		if step["state"] != "done" || step["proof_ref"] == nil {
			continue
		}
		no, _ := step["step_no"].(float64)
		out[int(no)] = step
	}
	return out
}

// TestDetailCarriesPlayableEvidence is the regression for the review drawer that showed a CEO
// "No proof media is attached to this action." on a round whose six videos and strip photo all
// existed. Admin-web resolved each ref through GET /app/proofs/{id}/download, which answers a
// GCS-backed deployment with a 307 to storage rather than the JSON envelope it decoded -- so
// every ref resolved to null and the verdict was asked for with no evidence on screen.
//
// The detail read now signs its own evidence, the way verification and weighing already do.
func TestDetailCarriesPlayableEvidence(t *testing.T) {
	media := &stubMedia{}
	body := getDetail(t, NewHandler(stubService{row: reviewedRound()}, nil).WithMediaResolver(media))

	if got := body["strip_photo_url"]; got != "https://storage.example/photo-7?sig=x" {
		t.Fatalf("strip_photo_url = %v, want the signed photo URL", got)
	}
	if got := body["strip_photo_mime"]; got != "image/jpeg" {
		t.Fatalf("strip_photo_mime = %v, want image/jpeg", got)
	}
	done := doneStepMedia(t, body)
	if len(done) != 3 {
		t.Fatalf("done steps = %d, want 3", len(done))
	}
	for no, step := range done {
		want := fmt.Sprintf("https://storage.example/video-%d?sig=x", no)
		if step["media_url"] != want {
			t.Fatalf("step %d media_url = %v, want %s", no, step["media_url"], want)
		}
		if step["media_mime"] != "video/mp4" {
			t.Fatalf("step %d media_mime = %v, want video/mp4", no, step["media_mime"])
		}
	}
	// Four refs, four resolves: the detail must not re-sign the same ref per step payload.
	if media.calls != 4 {
		t.Fatalf("resolver calls = %d, want 4 (3 videos + 1 photo)", media.calls)
	}
}

// TestOneUnreadableClipDoesNotBlankTheReview pins the degradation. An object that has gone
// missing must cost the reviewer that ONE clip -- not the strip photo, not the other videos,
// and not the whole detail read, which would leave her unable to reach the round at all.
func TestOneUnreadableClipDoesNotBlankTheReview(t *testing.T) {
	media := &stubMedia{broken: map[string]bool{"video-2": true}}
	body := getDetail(t, NewHandler(stubService{row: reviewedRound()}, nil).WithMediaResolver(media))

	done := doneStepMedia(t, body)
	if _, ok := done[2]["media_url"]; ok {
		t.Fatal("an unresolvable ref must carry no media_url")
	}
	if done[2]["proof_ref"] != "video-2" {
		t.Fatal("the step must still name its proof_ref so the gap is traceable")
	}
	if done[1]["media_url"] == nil || done[3]["media_url"] == nil {
		t.Fatal("a sibling step lost its media because one ref failed")
	}
	if body["strip_photo_url"] != "https://storage.example/photo-7?sig=x" {
		t.Fatal("the strip photo was dropped because a step video failed")
	}
}

// TestDetailWithoutAResolverStillServes keeps the seam optional: a deployment that has not
// wired media must still serve the round (proof_ref only), never 500 the review screen.
func TestDetailWithoutAResolverStillServes(t *testing.T) {
	body := getDetail(t, NewHandler(stubService{row: reviewedRound()}, nil))
	if _, ok := body["strip_photo_url"]; ok {
		t.Fatal("no resolver must mean no media URL, not a fabricated one")
	}
	if len(doneStepMedia(t, body)) != 3 {
		t.Fatal("the steps must still be served without a resolver")
	}
}
