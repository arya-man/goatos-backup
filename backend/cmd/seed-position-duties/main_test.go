package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// TestSeedFixtureVaccinationOwnerPositionsMapToVaccinationDuties pins module +
// capability mapping for the seed owner positions, and duty_type per the
// reminder-cadence audience documented in
// backend/internal/kernelstages/reminder_cadence.go: "park_head /
// preventive_care_manager / shed_manager (manager+ tiers) carry 'manage'".
// backup_manager stays 'execute' even at HR tier "manager": a backup slot
// covers the absent manager's TASKS, not their supervisory authority (see
// TestVaccinationManagerTierSeatsGetManageDuty for the fuller manage-audience
// case, including preventive_care_manager and the vaccination_operator_*
// execute-only exception).
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
	wantDutyType := map[string]string{
		"shed_manager":   "manage",
		"park_head":      "manage",
		"backup_manager": "execute",
	}
	for _, duty := range duties {
		if duty.moduleCode != "pc.vaccination" {
			t.Fatalf("%s module=%s, want pc.vaccination", duty.positionCode, duty.moduleCode)
		}
		if want := wantDutyType[duty.positionCode]; duty.dutyType != want {
			t.Fatalf("%s duty=%s, want %s", duty.positionCode, duty.dutyType, want)
		}
		if duty.capability != vaccinationExecuteCapability {
			t.Fatalf("%s capability=%s, want %s", duty.positionCode, duty.capability, vaccinationExecuteCapability)
		}
	}
}

// TestVaccinationManagerTierSeatsGetManageDuty locks in the reminder-cadence
// audience documented in backend/internal/kernelstages/reminder_cadence.go:
// "park_head / preventive_care_manager / shed_manager (manager+ tiers) carry
// 'manage'" for pc.vaccination, and reminderCadenceDutyTypes actively resolves
// both 'execute' and 'manage'. tools/dev/seed-closeout.sh's
// assert_reminder_audience_resolves check requires an active seat holding BOTH
// pc.vaccination duties -- so a fresh database is unseedable unless a genuine
// manager-tier pc.vaccination seat can earn 'manage'.
//
// Before this fix, deriveDuties excluded the ENTIRE pc.vaccination module from
// 'manage' (mp.moduleCode != "pc.vaccination"), not just the vaccination_operator_*
// seats the exclusion was written for (commit cae41af63). That starved the
// documented manage audience and made the closeout check unsatisfiable on any
// fresh seed.
func TestVaccinationManagerTierSeatsGetManageDuty(t *testing.T) {
	duties, st := deriveDuties([]positionRow{
		{positionCode: "preventive_care_manager", positionTier: "manager"},
		{positionCode: "park_head", positionTier: "head"},
		{positionCode: "shed_manager", positionTier: "manager"},
		// Vaccination operators keep 'execute' even at an inflated manager tier.
		{positionCode: "vaccination_operator_amit", positionTier: "manager"},
	})
	if st.UnmappedSkipped != 0 {
		t.Fatalf("unmapped=%d prefixes=%v, want all positions mapped", st.UnmappedSkipped, st.UnmappedPrefixes)
	}
	got := map[string]string{}
	for _, d := range duties {
		if d.moduleCode != "pc.vaccination" {
			t.Fatalf("%s module=%s, want pc.vaccination", d.positionCode, d.moduleCode)
		}
		got[d.positionCode] = d.dutyType
	}
	wantManage := []string{"preventive_care_manager", "park_head", "shed_manager"}
	for _, code := range wantManage {
		if got[code] != "manage" {
			t.Errorf("%s duty=%q, want manage (real reminder-cadence audience per reminder_cadence.go)", code, got[code])
		}
	}
	if got["vaccination_operator_amit"] != "execute" {
		t.Errorf("vaccination_operator_amit duty=%q, want execute despite manager tier", got["vaccination_operator_amit"])
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

// TestVerifierDutyModulesCoverEveryRegisteredVerificationModule pins the 2026-08-27 milk
// fix: the mobile verifier's per-feature [Verify, Alerts] bar is composed from these duty
// rows (workforce verifierFeatureKeys), so a verification category registered in
// verificationcatalog with no verify duty row is a queue the web lens shows and the phone
// never composes a tab for — 11 pending milk videos were invisible on the phone this way.
// Mutation test: deleting the verificationcatalog union in verifierDutyModules turns this
// red (milk has, deliberately, no notification profile).
func TestVerifierDutyModulesCoverEveryRegisteredVerificationModule(t *testing.T) {
	modules := verifierDutyModules()
	normalized := map[string]bool{}
	for _, m := range modules {
		key := strings.ReplaceAll(strings.TrimPrefix(m, "pc."), ".", "_")
		if normalized[key] {
			t.Errorf("module %q appears under two spellings; the union must dedupe on the normalized key", m)
		}
		normalized[key] = true
	}
	for _, def := range verificationcatalog.All() {
		key := strings.ReplaceAll(strings.TrimPrefix(def.NavigationModule, "pc."), ".", "_")
		if !normalized[key] {
			t.Errorf("registered verification module %q has no verify duty module; its queue would be web-only", def.NavigationModule)
		}
	}
	// The notification spellings must survive verbatim: ResolveModuleDutyRecipients joins on
	// them exactly, so "vaccination" beside a dropped "pc.vaccination" would silence pushes.
	for _, m := range notificationbridge.PendingNotificationDutyModules() {
		if !slices.Contains(modules, m) {
			t.Errorf("notified module %q missing from verifierDutyModules; its push would resolve to zero devices", m)
		}
	}
}
