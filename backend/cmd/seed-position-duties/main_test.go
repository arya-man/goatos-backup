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
	rows, err := deriveVerifierDuties(positions, modules)
	if err != nil {
		t.Fatalf("derive verifier duties: %v", err)
	}
	got := map[string]string{}
	for _, d := range rows {
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
	duties, err := deriveVerifierDuties(
		[]positionRow{{positionCode: "vaccination_operator_amit", positionTier: "manager"}},
		notificationbridge.PendingNotificationDutyModules(),
	)
	if err != nil {
		t.Fatalf("derive verifier duties: %v", err)
	}
	if len(duties) != 0 {
		t.Fatalf("derived %d verify duties with no verifier seat present; want none", len(duties))
	}
}

// TestDeriveVerifierDutiesScopesEachSeatToItsOwnModules is the seeding half of the queue's
// module gate. The gate can only refuse a module the caller is POSITIVELY known not to hold,
// so as long as every reviewer is seeded with every module it refuses nobody. This pins the
// narrowing: a module-scoped seat gets exactly one duty row, and no seat leaks a module
// belonging to another desk.
func TestDeriveVerifierDutiesScopesEachSeatToItsOwnModules(t *testing.T) {
	modules := notificationbridge.PendingNotificationDutyModules()
	positions := []positionRow{
		{positionCode: "video_verifier_weighing", positionTier: "manager"},
		{positionCode: "video_verifier_counts", positionTier: "manager"},
		{positionCode: "vaccination_operator_amit", positionTier: "manager"},
	}
	rows, err := deriveVerifierDuties(positions, modules)
	if err != nil {
		t.Fatalf("derive verifier duties: %v", err)
	}
	bySeat := map[string][]string{}
	for _, d := range rows {
		if d.dutyType != dutyTypeVerify {
			t.Errorf("seat %s got duty_type %q, want %q", d.positionCode, d.dutyType, dutyTypeVerify)
		}
		if d.capability != "" {
			t.Errorf("seat %s: a verify duty must confer no execution capability, got %q", d.positionCode, d.capability)
		}
		bySeat[d.positionCode] = append(bySeat[d.positionCode], d.moduleCode)
	}
	if len(bySeat) != 2 {
		t.Fatalf("seats with verify duty = %v, want exactly the two review seats", bySeat)
	}
	for seat, want := range map[string]string{
		"video_verifier_weighing": "weighing",
		"video_verifier_counts":   "counts",
	} {
		got := bySeat[seat]
		if len(got) != 1 || got[0] != want {
			t.Fatalf("seat %s holds %v, want only [%s]", seat, got, want)
		}
	}
}

// The bare seat keeps every module on purpose: it is the "reviews everything" desk, and it is
// the only review seat the shipped roster has. Narrowing it would strip modules of their only
// verify duty holder and send their pending-proof pushes to zero devices.
func TestDeriveVerifierDutiesKeepsTheBareSeatUnnarrowed(t *testing.T) {
	modules := notificationbridge.PendingNotificationDutyModules()
	rows, err := deriveVerifierDuties(
		[]positionRow{
			{positionCode: VerifierPositionCode, positionTier: "manager"},
			{positionCode: "video_verifier_counts", positionTier: "manager"},
		},
		modules,
	)
	if err != nil {
		t.Fatalf("derive verifier duties: %v", err)
	}
	held := map[string]bool{}
	for _, d := range rows {
		if d.positionCode == VerifierPositionCode {
			held[d.moduleCode] = true
		}
	}
	if len(held) != len(modules) {
		t.Fatalf("bare seat holds %v, want every notified module %v", held, modules)
	}
}

// A seat naming a module nothing notifies is a typo or a wiring gap. Granting it everything
// would re-open the very gap the scoping closes; granting it nothing would leave its holder
// unreachable. Both are silent, so the seed refuses to run.
func TestDeriveVerifierDutiesRejectsAnUnknownModuleSuffix(t *testing.T) {
	_, err := deriveVerifierDuties(
		[]positionRow{{positionCode: "video_verifier_procurement", positionTier: "manager"}},
		notificationbridge.PendingNotificationDutyModules(),
	)
	if err == nil {
		t.Fatal("a verifier seat naming an unnotified module must fail the seed, not be silently scoped")
	}
}

// A module-scoped review seat must not fall into the unmapped bucket either: both real
// invocations pass -strict, which aborts before insertDuties, so an unrecognised review seat
// would leave position_module_duties empty exactly as the bare seat once did.
func TestDeriveDutiesSkipsModuleScopedVerifierSeats(t *testing.T) {
	duties, st := deriveDuties([]positionRow{
		{positionCode: "video_verifier_weighing", positionTier: "manager"},
		{positionCode: "vaccination_operator_amit", positionTier: "manager"},
	})
	if st.UnmappedSkipped != 0 {
		t.Fatalf("UnmappedSkipped=%d (%v); a module-scoped review seat must not trip -strict", st.UnmappedSkipped, st.UnmappedPrefixes)
	}
	for _, d := range duties {
		if isVerifierSeat(d.positionCode) {
			t.Fatalf("deriveDuties emitted a %q duty for review seat %q; a reviewer must never hold execution duty", d.dutyType, d.positionCode)
		}
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
