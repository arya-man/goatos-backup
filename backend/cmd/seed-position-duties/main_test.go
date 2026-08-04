package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
)

func TestSeedFixtureVaccinationOwnerPositionsMapToVaccinationDuties(t *testing.T) {
	duties, st := deriveDuties([]positionRow{
		{positionCode: "shed_manager", positionTier: "manager"},
		{positionCode: "park_head", positionTier: "head"},
		{positionCode: "backup_manager", positionTier: "manager", isBackupSlot: true},
	})
	if st.UnmappedSkipped != 0 {
		t.Fatalf("unmapped=%d prefixes=%v, want all seed owner positions mapped", st.UnmappedSkipped, st.UnmappedPrefixes)
	}
	if len(duties) != 3 {
		t.Fatalf("duties=%d, want 3", len(duties))
	}
	for _, duty := range duties {
		if duty.moduleCode != "pc.vaccination" {
			t.Fatalf("%s module=%s, want pc.vaccination", duty.positionCode, duty.moduleCode)
		}
		if duty.dutyType != "execute" {
			t.Fatalf("%s duty=%s, want execute", duty.positionCode, duty.dutyType)
		}
		if duty.capability != vaccinationExecuteCapability {
			t.Fatalf("%s capability=%s, want %s", duty.positionCode, duty.capability, vaccinationExecuteCapability)
		}
	}
}

// TestDeriveVerifierDutiesCoversEveryNotifiedModule is the compile-time half of the
// seed-closeout assertion: the seeder must emit a 'verify' duty row for EVERY module that
// routes a pending-proof notification, from the notification map itself rather than from a
// hand-copied list.
//
// Before this, the seeder emitted only 'execute' and 'manage'. position_module_duties had
// zero verify rows, so ResolveModuleDutyRecipients' duty_type='verify' join matched nothing
// and every verifier push -- vaccination and weighing included -- resolved to zero devices
// while every test stayed green.
func TestDeriveVerifierDutiesCoversEveryNotifiedModule(t *testing.T) {
	modules := notificationbridge.PendingNotificationDutyModules()
	if len(modules) == 0 {
		t.Fatal("no notified duty modules; the notification profiles are empty")
	}
	positions := []positionRow{
		{positionCode: "vaccination_operator_amit", positionTier: "manager"},
		{positionCode: VerifierPositionCode, positionTier: "manager"},
	}
	got := map[string]string{}
	for _, d := range deriveVerifierDuties(positions, modules) {
		if d.positionCode != VerifierPositionCode {
			t.Errorf("verify duty attached to %q, want the verifier seat %q", d.positionCode, VerifierPositionCode)
		}
		if d.capability != "" {
			t.Errorf("module %s: verify duty must confer no execution capability, got %q", d.moduleCode, d.capability)
		}
		got[d.moduleCode] = d.dutyType
	}
	for _, module := range modules {
		if got[module] != dutyTypeVerify {
			t.Errorf("module %q has no '%s' duty row; its verifier push would resolve to zero devices", module, dutyTypeVerify)
		}
	}
}

// A verify duty is never invented for a seat nobody holds: the closeout assertion reports
// the gap instead, so the failure is "no verifier is seated" rather than a duty row that
// resolves to nobody.
func TestDeriveVerifierDutiesRequiresTheSeatToExist(t *testing.T) {
	duties := deriveVerifierDuties(
		[]positionRow{{positionCode: "vaccination_operator_amit", positionTier: "manager"}},
		notificationbridge.PendingNotificationDutyModules(),
	)
	if len(duties) != 0 {
		t.Fatalf("derived %d verify duties with no verifier seat present; want none", len(duties))
	}
}

// TestDeriveDutiesSkipsVerifierSeatWithoutMarkingItUnmapped locks in the fix for the seed
// that aborted before writing anything. The verifier seat matches no module prefix, so it
// used to land in the unmapped bucket -- and BOTH real invocations pass -strict (Makefile
// seed-vaccination-real and seed-vaccination-cpt-operator-drive), which returns an error
// BEFORE insertDuties. The result was an empty position_module_duties table and a verify
// join that matched nothing, i.e. exactly the failure the verify rows were added to fix.
//
// The seat must be SKIPPED, not mapped: giving it a module prefix would mint an 'execute'
// duty for the person who reviews the proof, collapsing separation of duty.
func TestDeriveDutiesSkipsVerifierSeatWithoutMarkingItUnmapped(t *testing.T) {
	duties, st := deriveDuties([]positionRow{
		{positionCode: "vaccination_operator_amit", positionTier: "manager"},
		{positionCode: VerifierPositionCode, positionTier: "manager"},
	})
	if st.UnmappedSkipped != 0 {
		t.Fatalf("UnmappedSkipped=%d (%v); the verifier seat must not trip -strict", st.UnmappedSkipped, st.UnmappedPrefixes)
	}
	for _, d := range duties {
		if d.positionCode == VerifierPositionCode {
			t.Fatalf("deriveDuties emitted a %q duty for the verifier seat; verify duties come from deriveVerifierDuties only", d.dutyType)
		}
	}
	if len(duties) != 1 {
		t.Fatalf("derived %d duties, want only the operator's", len(duties))
	}
}
