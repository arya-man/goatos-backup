package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

const (
	shiftTestTenant   = "00000000-0000-4000-8000-000000000001"
	shiftTestPark     = "20000000-0000-4000-8000-00000000000a"
	shiftTestOperator = "30000000-0000-4000-8000-000000000077"
)

type fakeShiftWriter struct {
	setErr   error
	clearErr error
	sets     []vaccexecd.OperatorShiftInput
	lastReq  vaccexecapp.OperatorShiftRequest
}

func (f *fakeShiftWriter) ListOperatorShifts(context.Context, string, string) ([]vaccexecd.OperatorShift, error) {
	return nil, nil
}

func (f *fakeShiftWriter) SetOperatorShift(_ context.Context, req vaccexecapp.OperatorShiftRequest, in vaccexecd.OperatorShiftInput) (vaccexecd.OperatorShift, bool, *vaccexecd.OperatorShiftFieldError, error) {
	f.lastReq = req
	shift, ferr := vaccexecd.ValidateOperatorShiftInput(in)
	if ferr != nil {
		return vaccexecd.OperatorShift{}, false, ferr, nil
	}
	f.sets = append(f.sets, in)
	if f.setErr != nil {
		return vaccexecd.OperatorShift{}, false, nil, f.setErr
	}
	return shift, false, nil, nil
}

func (f *fakeShiftWriter) ClearOperatorShift(_ context.Context, req vaccexecapp.OperatorShiftRequest, _, _ string) (bool, error) {
	f.lastReq = req
	return false, f.clearErr
}

func serveShift(t *testing.T, w *fakeShiftWriter, method, target, body, key string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, NewHandler(&fakeReader{}, &fakeWriter{}).WithOperatorShiftWriter(w))
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	ctx := httpmiddleware.WithTenantID(req.Context(), shiftTestTenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: shiftTestTenant}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func shiftBody(start, end string) string {
	return `{"park_id":"` + shiftTestPark + `","operator_id":"` + shiftTestOperator + `","shift_label":"am","shift_start":"` + start + `","shift_end":"` + end + `","week_off_weekday":"sunday"}`
}

func TestPutOperatorShiftRequiresAnIdempotencyKey(t *testing.T) {
	w := &fakeShiftWriter{}
	rec, body := serveShift(t, w, http.MethodPut, "/vaccination/operator-shifts", shiftBody("08:00", "17:00"), "")
	if rec.Code != http.StatusBadRequest || body["code"] != "missing_idempotency_key" || len(w.sets) != 0 {
		t.Fatalf("got %d %v writes=%d", rec.Code, body, len(w.sets))
	}
}

func TestPutOperatorShiftNamesTheRefusedField(t *testing.T) {
	w := &fakeShiftWriter{}
	rec, body := serveShift(t, w, http.MethodPut, "/vaccination/operator-shifts", shiftBody("08:00", "7am"), "key-00000001")
	if rec.Code != http.StatusBadRequest || body["field"] != "shift_end" || body["code"] != "invalid_shift_end" {
		t.Fatalf("got %d %v", rec.Code, body)
	}
}

func TestPutOperatorShiftWritesAndCarriesTheKey(t *testing.T) {
	w := &fakeShiftWriter{}
	rec, body := serveShift(t, w, http.MethodPut, "/vaccination/operator-shifts", shiftBody("08:00", "17:00"), "key-00000002")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	if w.lastReq.IdempotencyKey != "key-00000002" || w.lastReq.TenantID != shiftTestTenant || len(w.sets) != 1 || w.sets[0].ParkID != shiftTestPark {
		t.Fatalf("unexpected write: %+v %+v", w.lastReq, w.sets)
	}
	shift, _ := body["shift"].(map[string]any)
	if shift["shiftStartMinute"] != float64(480) || shift["shiftEndMinute"] != float64(1020) {
		t.Fatalf("unexpected shift body: %v", body)
	}
}

func TestPutOperatorShiftMapsRepositoryRefusals(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{vaccexecapp.ErrOperatorNotActiveInPark, http.StatusBadRequest, "operator_not_in_park"},
		{vaccexecapp.ErrOperatorShiftIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
	}
	for _, tc := range cases {
		w := &fakeShiftWriter{setErr: tc.err}
		rec, body := serveShift(t, w, http.MethodPut, "/vaccination/operator-shifts", shiftBody("08:00", "17:00"), "key-00000003")
		if rec.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("%v: got %d %v", tc.err, rec.Code, body)
		}
	}
}

func TestDeleteOperatorShiftRefusesWhileTheParkAssignmentUsesTheOperator(t *testing.T) {
	w := &fakeShiftWriter{clearErr: vaccexecapp.ErrOperatorShiftInUse}
	target := "/vaccination/operator-shifts?park_id=" + shiftTestPark + "&operator_id=" + shiftTestOperator
	rec, body := serveShift(t, w, http.MethodDelete, target, "", "key-00000004")
	if rec.Code != http.StatusConflict || body["code"] != "operator_shift_in_use" {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "default or selected drive operator") {
		t.Fatalf("the refusal must say why: %q", msg)
	}
}

func TestDeleteOperatorShiftClears(t *testing.T) {
	w := &fakeShiftWriter{}
	target := "/vaccination/operator-shifts?park_id=" + shiftTestPark + "&operator_id=" + shiftTestOperator
	rec, body := serveShift(t, w, http.MethodDelete, target, "", "key-00000005")
	if rec.Code != http.StatusOK || body["cleared"] != true || body["operatorId"] != shiftTestOperator {
		t.Fatalf("got %d %v", rec.Code, body)
	}
}
