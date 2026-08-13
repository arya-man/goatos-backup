package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestHealthDepartmentReceivesHealthModuleByDefault(t *testing.T) {
	modules := defaultDepartmentModules["health"]
	want := map[string]bool{
		"aas_health":     true,
		"counts":         true,
		"milk":           true,
		"feed_direction": true,
		"vaccination":    true,
	}
	if len(modules) != len(want) {
		t.Fatalf("health default modules = %v, want exactly Health + Counts + Milk + Feed + Vaccination", modules)
	}
	for _, module := range modules {
		if !want[module] {
			t.Fatalf("health default modules = %v, unexpected module %q", modules, module)
		}
		delete(want, module)
	}
	if len(want) != 0 {
		t.Fatalf("health default modules = %v, missing %v", modules, want)
	}
}

// TestDefaultDepartmentModulesMatchDecisions pins the seed module map EXACTLY, one entry per
// recorded maintainer decision. It replaces TestCountsDepartmentsAlsoReceiveMilkModule, which
// asserted the weaker invariant "any department granted counts is also granted milk".
//
// Why the invariant had to go rather than be relaxed: it encoded the 2026-07-31 module split,
// where Milk Prep and Milk Feeding moved out of Counts into their own "milk" drawer module while
// keeping their /counts/... routes and CountsWrite authority. Losing them from a bar was always a
// migration accident then, so counts-implies-milk was a safe proxy. Maintainer decision 2026-08-05
// removes milk from preventive_care ON PURPOSE, which makes the proxy wrong: it would fail a
// deliberate decision while still passing any typo that dropped a module it did not name.
//
// An exact map is strictly stronger. It catches the accidental loss the old test caught (a dropped
// module fails the length check), and it also catches an accidental ADDITION, which the old test
// could not see at all. Every future change to the map is a one-line diff here plus a decision
// note in defaultDepartmentModules' doc comment.
func TestDefaultDepartmentModulesMatchDecisions(t *testing.T) {
	want := map[string][]string{
		// Maintainer decision 2026-08-05: NO milk and NO aas_health. A PC seat's bar is
		// Vaccination + Counts + Feed. Milk Prep / Milk Feeding intentionally do not appear.
		"preventive_care": {"vaccination", "counts", "feed_direction"},
		// Maintainer decision 2026-07-30, unchanged by the 2026-08-05 PC decision.
		"health":   {"aas_health", "counts", "milk", "feed_direction", "vaccination"},
		"feed":     {"feed_direction"},
		"breeding": {"breeding"},
	}
	if len(defaultDepartmentModules) != len(want) {
		t.Fatalf("defaultDepartmentModules has %d departments, want %d: %v",
			len(defaultDepartmentModules), len(want), defaultDepartmentModules)
	}
	for department, wantModules := range want {
		gotModules, ok := defaultDepartmentModules[department]
		if !ok {
			t.Fatalf("department %q missing from defaultDepartmentModules", department)
		}
		got := map[string]bool{}
		for _, module := range gotModules {
			got[module] = true
		}
		if len(got) != len(gotModules) {
			t.Fatalf("department %q has duplicate modules: %v", department, gotModules)
		}
		for _, module := range wantModules {
			if !got[module] {
				t.Fatalf("department %q modules = %v, missing %q", department, gotModules, module)
			}
			delete(got, module)
		}
		for module := range got {
			t.Fatalf("department %q modules = %v, unexpected %q", department, gotModules, module)
		}
	}
}

// TestPreventiveCareDropsMilkAndHealth is the named regression for maintainer decision 2026-08-05.
// The exact-map test above would already fail if either module came back, but it fails with a
// generic "unexpected module" message; this one states WHY the module must stay out, so a future
// author who re-adds milk to satisfy the retired counts-implies-milk rule reads the reason in the
// failure itself rather than re-deriving it from git history.
func TestPreventiveCareDropsMilkAndHealth(t *testing.T) {
	for _, module := range defaultDepartmentModules["preventive_care"] {
		if module == "milk" || module == "aas_health" {
			t.Fatalf("preventive_care regained %q: maintainer decision 2026-08-05 removed milk and "+
				"aas_health from the PC bottom bar. The retired counts-implies-milk coupling is NOT a "+
				"reason to add it back; health keeps both modules instead.", module)
		}
	}
}

func TestValidateStrictRosterRejectsUnresolvedAndMissingOwners(t *testing.T) {
	mappings := []rosterMappingRow{{center: "CPT", position: "Preventive Care Manager"}}
	st := stats{MappingRows: 1, PositionSlotsDefined: 1, AssignmentsUnresolved: 1}
	err := validateStrictRoster(mappings, nil, st, nil)
	if err == nil {
		t.Fatal("strict validation accepted an unresolved, ownerless roster")
	}
	for _, want := range []string{"unresolved assignments=1", "missing resolved CPT/preventive_care_manager", "missing resolved CPT/park_head"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("strict error %q missing %q", err, want)
		}
	}
}

func TestValidateStrictRosterAcceptsRequiredCenterOwnership(t *testing.T) {
	var mappings []rosterMappingRow
	var assignments []rosterAssignment
	for _, center := range []string{"CBE", "CPT"} {
		for _, position := range []string{"Preventive Care Manager", "Backup Manager", "Park Head"} {
			mappings = append(mappings, rosterMappingRow{center: center, position: position})
			assignments = append(assignments, rosterAssignment{
				center: center, position: positionDefs[position], isResolved: true,
			})
		}
	}
	st := stats{MappingRows: len(assignments), PositionSlotsDefined: len(assignments)}
	if err := validateStrictRoster(mappings, assignments, st, nil); err != nil {
		t.Fatalf("strict validation rejected complete ownership: %v", err)
	}
}

// cptSeatAssignments returns the three resolved jun-26 CPT seats (PC-manager,
// backup-manager, park-head) mapped to Darshan/Sagar/Amit, exactly as the
// CPT operator-drive source CSV resolves before the overlay.
func cptSeatAssignments() []rosterAssignment {
	return []rosterAssignment{
		{center: "CPT", position: positionDefs["Preventive Care Manager"], jun26Name: "Darshan", isResolved: true, weekOffWeekday: "sunday"},
		{center: "CPT", position: positionDefs["Backup Manager"], jun26Name: "Sagar", isResolved: true, weekOffWeekday: "monday"},
		{center: "CPT", position: positionDefs["Park Head"], jun26Name: "Amit", isResolved: true, weekOffWeekday: "tuesday"},
	}
}

func cptContract() *operatorRosterContract {
	c := &operatorRosterContract{}
	c.SourceScope.ParkCode = "CPT"
	defaultCap := 200
	c.OperatorCapacity.DefaultAnimalsPerDay = &defaultCap
	c.OperatorAndroidLogin = &operatorRosterAndroidLogin{
		RequiredAfterDatabaseSeed:           true,
		IdentityProvider:                    "firebase_email_password",
		SourceEmailField:                    "operators[].email_hint",
		UniqueEmailPerOperator:              true,
		UniqueTemporaryPasswordPerOperator:  true,
		SharedPasswordForbidden:             true,
		PlaintextPasswordsInGitForbidden:    true,
		MustSendOrRecordIndividualResetFlow: true,
		AndroidLoginSmokeRequired:           true,
	}
	c.Operators = []operatorRosterOperator{
		{Code: "vaccination_operator_amit", DisplayName: "Amit Kumar", EmailHint: "amit@example.test", Tier: "manager", WeekOff: "friday"},
		{Code: "vaccination_operator_darshan", DisplayName: "Darshan Talwar", EmailHint: "darshan@example.test", Tier: "manager", WeekOff: "sunday"},
		{Code: "vaccination_operator_sagar", DisplayName: "Sagar Mahoor", EmailHint: "sagar@example.test", Tier: "manager", WeekOff: "saturday"},
	}
	return c
}

func TestApplyOperatorRosterOverlayRecastsSeats(t *testing.T) {
	assignments := cptSeatAssignments()
	centers, err := applyOperatorRosterOverlay(cptContract(), assignments)
	if err != nil {
		t.Fatalf("overlay errored on a complete contract: %v", err)
	}
	if !centers["CPT"] {
		t.Fatalf("expected CPT to be a contract-covered center, got %v", centers)
	}
	want := map[string]struct {
		code    string
		weekOff string
	}{
		"Amit":    {"vaccination_operator_amit", "friday"},
		"Darshan": {"vaccination_operator_darshan", "sunday"},
		"Sagar":   {"vaccination_operator_sagar", "saturday"},
	}
	for _, a := range assignments {
		w, ok := want[a.jun26Name]
		if !ok {
			t.Fatalf("unexpected assignment for %q", a.jun26Name)
		}
		if a.position.code != w.code {
			t.Errorf("%s: position code = %q, want %q", a.jun26Name, a.position.code, w.code)
		}
		if a.position.tier != "manager" {
			t.Errorf("%s: tier = %q, want manager", a.jun26Name, a.position.tier)
		}
		if a.position.isBackupSlot {
			t.Errorf("%s: is a backup slot; equal operators must not be backup", a.jun26Name)
		}
		if a.weekOffWeekday != w.weekOff {
			t.Errorf("%s: week-off = %q, want %q", a.jun26Name, a.weekOffWeekday, w.weekOff)
		}
		if a.vaccinationCap == nil || *a.vaccinationCap != 200 {
			t.Errorf("%s: vaccination cap = %v, want 200", a.jun26Name, a.vaccinationCap)
		}
	}
}

func TestApplyOperatorRosterOverlayUsesPerOperatorCap(t *testing.T) {
	assignments := cptSeatAssignments()
	contract := cptContract()
	amitCap := 1
	contract.Operators[0].AnimalCapPerDay = &amitCap

	if _, err := applyOperatorRosterOverlay(contract, assignments); err != nil {
		t.Fatalf("overlay errored on per-operator cap: %v", err)
	}
	for _, a := range assignments {
		if a.jun26Name == "Amit" {
			if a.vaccinationCap == nil || *a.vaccinationCap != 1 {
				t.Fatalf("Amit cap = %v, want per-operator cap 1", a.vaccinationCap)
			}
			continue
		}
		if a.vaccinationCap == nil || *a.vaccinationCap != 200 {
			t.Fatalf("%s cap = %v, want default cap 200", a.jun26Name, a.vaccinationCap)
		}
	}
}

func TestApplyOperatorRosterOverlayErrorsOnUnmatchedOperator(t *testing.T) {
	// Only two of the three declared operators have a resolved seat.
	assignments := cptSeatAssignments()[:2]
	_, err := applyOperatorRosterOverlay(cptContract(), assignments)
	if err == nil {
		t.Fatal("overlay accepted a contract operator with no resolved seat")
	}
	if !strings.Contains(err.Error(), "vaccination_operator_amit") {
		t.Fatalf("error %q should name the unmatched operator vaccination_operator_amit", err)
	}
}

func TestApplyOperatorRosterOverlayNilContractIsNoOp(t *testing.T) {
	assignments := cptSeatAssignments()
	centers, err := applyOperatorRosterOverlay(nil, assignments)
	if err != nil {
		t.Fatalf("nil contract should be a no-op, got %v", err)
	}
	if len(centers) != 0 {
		t.Fatalf("nil contract should cover no centers, got %v", centers)
	}
	if assignments[0].position.code != "preventive_care_manager" {
		t.Fatalf("nil contract must not mutate seats, got %q", assignments[0].position.code)
	}
}

func TestValidateStrictRosterWaivesContractCoveredCenter(t *testing.T) {
	// After the overlay, CPT has only vaccination_operator_* seats and no
	// PC-manager/backup/park-head trio; strict must pass because CPT is a
	// contract-covered center.
	assignments := cptSeatAssignments()
	if _, err := applyOperatorRosterOverlay(cptContract(), assignments); err != nil {
		t.Fatalf("overlay setup failed: %v", err)
	}
	mappings := []rosterMappingRow{
		{center: "CPT", position: "Preventive Care Manager"},
		{center: "CPT", position: "Backup Manager"},
		{center: "CPT", position: "Park Head"},
	}
	st := stats{MappingRows: 3, PositionSlotsDefined: 3}
	if err := validateStrictRoster(mappings, assignments, st, map[string]bool{"CPT": true}); err != nil {
		t.Fatalf("strict validation rejected a contract-covered operator-roster center: %v", err)
	}
}

// --- BUG-024: contract blocks must be consumed, never silently dropped ---

const cptRosterFixture = "../../../fixtures/vaccination-cpt-operator-drive-2026-07-23"

func TestLoadOperatorRosterConsumesDirectorsAndLeadership(t *testing.T) {
	contract, err := loadOperatorRoster(cptRosterFixture)
	if err != nil {
		t.Fatalf("load committed CPT roster contract: %v", err)
	}
	if contract == nil {
		t.Fatal("expected the committed CPT roster contract to load")
	}
	if len(contract.Directors) != 1 || contract.Directors[0].Code != "preventive_care_director_chandrakant" {
		t.Fatalf("directors block dropped or wrong: %+v", contract.Directors)
	}
	if contract.Directors[0].CanExecuteVaccinaton {
		t.Fatal("director must not declare execution capacity")
	}
	if len(contract.Verifiers) != 1 || contract.Verifiers[0].Email != "jyothipvg12345@gmail.com" {
		t.Fatalf("verifiers block dropped or wrong: %+v", contract.Verifiers)
	}
	if contract.Verifiers[0].Role != "verifier" || contract.Verifiers[0].CanExecuteVaccinaton || contract.Verifiers[0].AddsVaccinationCapacity {
		t.Fatalf("verifier must be verifier-only with no capacity: %+v", contract.Verifiers[0])
	}
	if contract.LeadershipFullAccess == nil {
		t.Fatal("leadership_full_access block dropped")
	}
	if got := len(contract.LeadershipFullAccess.Emails); got != 5 {
		t.Fatalf("leadership_full_access.emails = %d, want 5", got)
	}
	if contract.LeadershipFullAccess.GrantRole != "ceo_internal" {
		t.Fatalf("grant_role = %q, want ceo_internal", contract.LeadershipFullAccess.GrantRole)
	}
	if contract.OperatorAndroidLogin == nil {
		t.Fatal("operator_android_login block dropped")
	}
	if !contract.OperatorAndroidLogin.UniqueTemporaryPasswordPerOperator || !contract.OperatorAndroidLogin.SharedPasswordForbidden {
		t.Fatalf("operator_android_login password contract too weak: %+v", contract.OperatorAndroidLogin)
	}
	seen := map[string]string{}
	for _, op := range contract.Operators {
		email := strings.ToLower(strings.TrimSpace(op.EmailHint))
		if email == "" || !strings.Contains(email, "@") {
			t.Fatalf("operator %s missing Android login email_hint", op.Code)
		}
		if previous := seen[email]; previous != "" {
			t.Fatalf("operators %s and %s share Android login email %s", previous, op.Code, email)
		}
		seen[email] = op.Code
	}
}

func TestLoadOperatorRosterRejectsUnconsumedBlock(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(cptRosterFixture + "/cpt-operator-roster.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	// A future contract block nobody seeds must be a loud failure, not a silent drop.
	generic["shed_operator_overrides"] = []any{map[string]any{"shed": "Gandhi", "operator": "x"}}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(dir+"/cpt-operator-roster.json", out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = loadOperatorRoster(dir)
	if err == nil {
		t.Fatal("expected an unconsumed contract block to be a hard error")
	}
	if !strings.Contains(err.Error(), "shed_operator_overrides") {
		t.Fatalf("error must name the unconsumed block, got: %v", err)
	}
}

func TestLoadOperatorRosterRejectsDirectorWithExecutionCapacity(t *testing.T) {
	dir := t.TempDir()
	raw, _ := os.ReadFile(cptRosterFixture + "/cpt-operator-roster.json")
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	generic["directors"].([]any)[0].(map[string]any)["can_execute_vaccination"] = true
	out, _ := json.Marshal(generic)
	_ = os.WriteFile(dir+"/cpt-operator-roster.json", out, 0o600)
	if _, err := loadOperatorRoster(dir); err == nil || !strings.Contains(err.Error(), "can_execute_vaccination") {
		t.Fatalf("expected director execution-capacity rejection, got: %v", err)
	}
}

func TestLoadOperatorRosterRejectsDuplicateOperatorAndroidEmail(t *testing.T) {
	dir := t.TempDir()
	raw, _ := os.ReadFile(cptRosterFixture + "/cpt-operator-roster.json")
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	operators := generic["operators"].([]any)
	operators[1].(map[string]any)["email_hint"] = operators[0].(map[string]any)["email_hint"]
	out, _ := json.Marshal(generic)
	_ = os.WriteFile(dir+"/cpt-operator-roster.json", out, 0o600)
	if _, err := loadOperatorRoster(dir); err == nil || !strings.Contains(err.Error(), "Android operator logins must be unique") {
		t.Fatalf("expected duplicate Android email rejection, got: %v", err)
	}
}

func TestLoadOperatorRosterRejectsSharedPasswordContract(t *testing.T) {
	dir := t.TempDir()
	raw, _ := os.ReadFile(cptRosterFixture + "/cpt-operator-roster.json")
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	generic["operator_android_login"].(map[string]any)["unique_temporary_password_per_operator"] = false
	out, _ := json.Marshal(generic)
	_ = os.WriteFile(dir+"/cpt-operator-roster.json", out, 0o600)
	if _, err := loadOperatorRoster(dir); err == nil || !strings.Contains(err.Error(), "unique_temporary_password_per_operator") {
		t.Fatalf("expected weak Android password contract rejection, got: %v", err)
	}
}

// TestDeriveRoleHintVaccinationOperatorAlwaysOperator locks the fix for the CPT
// operator-drive incident: a vaccination-executing operator must resolve to
// primary_role_hint="operator" even when their frozen June seat is a manager tier
// (would map to "supervisor") or a park_head seat (would map to "park_head"). The
// mobile scan gate keys on == "operator", so a wrong hint silently drops every scan.
func TestDeriveRoleHintVaccinationOperatorAlwaysOperator(t *testing.T) {
	mgr := "manager"
	cases := []struct {
		name     string
		member   memberRec
		grade    *string
		execVacc bool
		wantHint string
	}{
		{"manager-tier vacc operator (Amit/Darshan)", memberRec{bestCode: "vaccination_operator_amit", bestTier: "manager"}, &mgr, true, "operator"},
		{"park_head-seat vacc operator (Sagar)", memberRec{bestCode: "park_head", bestTier: "head"}, &mgr, true, "operator"},
		{"non-operator manager stays supervisor", memberRec{bestCode: "breeding_manager", bestTier: "manager"}, &mgr, false, "supervisor"},
		{"non-operator park_head stays park_head", memberRec{bestCode: "park_head", bestTier: "head"}, nil, false, "park_head"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.member
			if got := deriveRoleHint(&m, tc.grade, tc.execVacc); got != tc.wantHint {
				t.Fatalf("deriveRoleHint = %q, want %q", got, tc.wantHint)
			}
		})
	}
}
