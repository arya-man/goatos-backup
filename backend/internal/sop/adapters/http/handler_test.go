package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListHandlersRejectInvalidLimitBeforeRepositoryWork(t *testing.T) {
	h := NewHandler(nil)
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{name: "sops", path: "/admin/sops?limit=garbage", handler: h.ListSOPs},
		{name: "admin tasks", path: "/admin/tasks?limit=0", handler: h.ListAdminTasks},
		{name: "app tasks", path: "/app/tasks?limit=-1", handler: h.ListAppTasks},
		{name: "app tasks over mobile boundary", path: "/app/tasks?limit=21", handler: h.ListAppTasks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.handler(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"invalid_limit"`) {
				t.Fatalf("body=%s, want invalid_limit", rec.Body.String())
			}
		})
	}
}

func TestListAppTasksRejectsMalformedCursorBeforeRepositoryWork(t *testing.T) {
	h := NewHandler(nil)
	rec := httptest.NewRecorder()
	h.ListAppTasks(rec, httptest.NewRequest(http.MethodGet, "/app/tasks?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid_cursor"`) {
		t.Fatalf("body=%s, want invalid_cursor", rec.Body.String())
	}
}

func TestListSOPsRejectsOversizedFiltersBeforeRepositoryWork(t *testing.T) {
	h := NewHandler(nil)
	for _, param := range []string{"q", "code_prefix"} {
		rec := httptest.NewRecorder()
		path := "/admin/sops?" + param + "=" + strings.Repeat("x", maxSOPListFilterLength+1)
		h.ListSOPs(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d want 400 body=%s", param, rec.Code, rec.Body.String())
		}
	}
}

func TestParsePositiveLimit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{raw: "", want: 0, ok: true},
		{raw: " 25 ", want: 25, ok: true},
		{raw: "garbage", ok: false},
		{raw: "0", ok: false},
		{raw: "-1", ok: false},
	} {
		got, ok := parsePositiveLimit(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("parsePositiveLimit(%q)=(%d,%v), want (%d,%v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
