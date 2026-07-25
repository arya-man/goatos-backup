package ceoai

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type stubExec struct{}

func (stubExec) Spec() ports.ToolSpec {
	return ports.ToolSpec{Name: "counts_breakdown", Route: domain.RouteAPI}
}
func (stubExec) Execute(_ context.Context, _ domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	return domain.ToolResult{Surface: "Mesha read API", ToolName: sub.ToolName,
		Facts: []domain.Fact{{Label: "animals", Value: "2567"}}}, nil
}

// Build with no Vertex provider must yield a working fallback assistant.
func TestBuildFallbackEndToEnd(t *testing.T) {
	svc := Build(Options{ReadTools: []ports.ToolExecutor{stubExec{}}})
	ans, err := svc.Assistant.Ask(context.Background(), domain.Question{
		Actor: domain.Actor{TenantID: "t1", UserID: "u1", Role: permissions.RoleCEOInternal},
		Text:  "count by shed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModeFallback {
		t.Fatalf("no provider => fallback mode, got %q", ans.Mode)
	}
	if ans.Answer == "" {
		t.Fatal("expected an answer body")
	}
}
