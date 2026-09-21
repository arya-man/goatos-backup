package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// A bucket the read does not know is REFUSED, on both Time-wise reads, rather than quietly
// resolved to weeks. Defaulting it would put calendar-week columns under a heading the reader
// selected as 30-day blocks -- a screen with no true number on it, which is the shape of defect
// this codebase's cross-surface rules exist to stop.
func TestUnknownGainBucketIsRefusedOnBothTimeWiseReads(t *testing.T) {
	service := NewService(&fakeRepo{})
	actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	ctx := context.Background()

	if _, err := service.GetWeightDemographics(ctx, actor, "", "", "", "", "", "", "", nil, "quarter", "", ""); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("demographics with an unknown bucket: err = %v, want ErrInvalidArgument", err)
	}
	if _, err := service.GetLeadershipGrowthADG(ctx, actor, "", "", "", "", "", "", "", "fortnight", "", ""); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("growth with an unknown bucket: err = %v, want ErrInvalidArgument", err)
	}
}

// A pen that is not a uuid is REFUSED, not ignored. Ignoring it would widen the read back to every
// pen while the page's own heading and its pen control still name one -- the whole farm's growth
// read as one pen's. A BLANK pen is the opposite case and is legitimate: it is every pen, the query
// these reads ran before the control existed. A blank PARTITION is legitimate too -- that is an
// undivided shed.
func TestAMalformedPenIsRefusedWhileABlankOneIsEveryPen(t *testing.T) {
	service := NewService(&fakeRepo{})
	actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	ctx := context.Background()

	if _, err := service.GetWeightDemographics(ctx, actor, "", "", "", "", "", "", "", nil, "", "Castro 1", ""); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("a pen NAME is not a pen id: err = %v, want ErrInvalidArgument", err)
	}
	if _, err := service.GetLeadershipGrowthADG(ctx, actor, "", "", "", "", "", "", "", "", "not-a-uuid", ""); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("growth with a malformed pen: err = %v, want ErrInvalidArgument", err)
	}
	// Blank pen, blank partition: accepted and resolved to no narrowing.
	scope, ok := resolveTimeScope("", "", "")
	if !ok || scope.PenSelected() {
		t.Fatalf("a blank pen must resolve to every pen, got %+v ok=%v", scope, ok)
	}
	scope, ok = resolveTimeScope("month", "b7ad4d0f-6d1b-4a4c-9c2f-4c2d9c3f7a11", "")
	if !ok || !scope.PenSelected() || scope.Bucket != domain.GainBucketMonth {
		t.Fatalf("an undivided shed is a pen with a blank partition, got %+v ok=%v", scope, ok)
	}
}

// A caller with no weighing authority is refused as UNAUTHORIZED even when its bucket is also
// wrong: the argument check must never run first and tell an outsider which arguments the read
// takes.
func TestBucketValidationNeverPreemptsThePermissionCheck(t *testing.T) {
	service := NewService(&fakeRepo{})
	outsider := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	ctx := context.Background()

	if _, err := service.GetWeightDemographics(ctx, outsider, "", "", "", "", "", "", "", nil, "quarter", "", ""); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("demographics: err = %v, want ErrForbidden", err)
	}
	if _, err := service.GetLeadershipGrowthADG(ctx, outsider, "", "", "", "", "", "", "", "quarter", "", ""); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("growth: err = %v, want ErrForbidden", err)
	}
}
