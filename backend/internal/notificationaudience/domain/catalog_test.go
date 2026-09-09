package domain

import (
	"reflect"
	"regexp"
	"testing"
)

var alertKeyRe = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)

// knownDesignations is the designation_catalog vocabulary the defaults may name (migrations
// 000219, 000247 and 000287). A default naming a code outside it would resolve to nobody, silently.
var knownDesignations = map[string]struct{}{
	DesignationCEO: {}, DesignationPCDirector: {}, DesignationGrowthDirector: {}, DesignationFeedDirector: {},
	DesignationHealthDirector: {}, DesignationProcurementDirector: {}, DesignationBreedingDirector: {},
	DesignationProcurementManager: {}, DesignationParkHead: {}, DesignationVerifier: {}, DesignationOperator: {},
	DesignationHR: {},
}

func TestCatalogRowsAreWellFormedAndUnique(t *testing.T) {
	seen := map[string]struct{}{}
	modules := map[string]struct{}{}
	for _, m := range Modules {
		modules[m.Key] = struct{}{}
	}
	for _, alert := range Catalog() {
		if !alertKeyRe.MatchString(alert.Key) {
			t.Errorf("alert key %q must be <module>.<alert> (the notification_alert_audiences CHECK refuses anything else)", alert.Key)
		}
		if _, dup := seen[alert.Key]; dup {
			t.Errorf("alert key %q appears twice", alert.Key)
		}
		seen[alert.Key] = struct{}{}
		if _, ok := modules[alert.Module]; !ok {
			t.Errorf("alert %q names module %q, which is not in Modules", alert.Key, alert.Module)
		}
		if alert.Label == "" || alert.Blurb == "" {
			t.Errorf("alert %q must carry admin-facing label and blurb", alert.Key)
		}
		if len(alert.DefaultDesignations) == 0 {
			t.Errorf("alert %q has no default audience; deploying it would silence a push that used to reach someone", alert.Key)
		}
		for _, code := range alert.DefaultDesignations {
			if _, ok := knownDesignations[code]; !ok {
				t.Errorf("alert %q defaults to %q, which is not a designation_catalog code", alert.Key, code)
			}
		}
	}
}

// TestEveryDeclaredAlertKeyIsInTheCatalog pins that a constant a notifier can ask for always
// resolves: an Alert* constant missing from the catalog makes the resolver refuse the key and
// the push silently reaches nobody.
func TestEveryDeclaredAlertKeyIsInTheCatalog(t *testing.T) {
	for _, key := range []string{
		AlertVaccinationDueTodayLeadership, AlertVaccinationWorkMissed, AlertVaccinationDriveReady, AlertVaccinationDriveClosed,
		AlertWeighingPlanPublished, AlertWeighingSubmitted, AlertWeighingReopened, AlertWeighingVerdictApprove,
		AlertWeighingVerdictRework, AlertWeighingPenClosed, AlertWeighingTaskClosed, AlertWeighingWorkCadence,
		AlertFeedLowStock, AlertFeedSaleReduce, AlertProcurementLoadOverdue, AlertLeadershipTaskRaised, AlertLeadershipTaskDone,
	} {
		if _, ok := AlertByKey(key); !ok {
			t.Errorf("alert constant %q is not in the catalog", key)
		}
	}
	for _, module := range []string{"vaccination", "weighing", "feed", "pc_care", "health", "counts"} {
		for _, suffix := range []string{ProofPendingSuffix, ProofApprovedSuffix, ProofReworkSuffix, ProofReviewSuffix} {
			if _, ok := AlertByKey(ProofAlertKey(module, suffix)); !ok {
				t.Errorf("proof alert %q is not in the catalog", ProofAlertKey(module, suffix))
			}
		}
	}
	if _, ok := AlertByKey("nosuch.alert"); ok {
		t.Fatal("unknown key must not resolve")
	}
}

// TestDefaultsReproduceThePreCatalogAudiences pins the audiences the notifiers resolved by hand
// before this catalog existed, in the order they resolved them. Changing one is a maintainer
// decision about who is told, not a refactor.
func TestDefaultsReproduceThePreCatalogAudiences(t *testing.T) {
	want := map[string][]string{
		AlertFeedLowStock:                            {DesignationCEO, DesignationFeedDirector, DesignationProcurementDirector},
		AlertProcurementLoadOverdue:                  {DesignationCEO},
		AlertFeedSaleReduce:                          {DesignationFeedDirector},
		AlertLeadershipTaskRaised:                    {DesignationCEO},
		ProofAlertKey("health", ProofReviewSuffix):   {DesignationVerifier},
		AlertVaccinationDueTodayLeadership:           {DesignationPCDirector, DesignationCEO},
		AlertVaccinationWorkMissed:                   {DesignationParkHead, DesignationPCDirector},
		AlertVaccinationDriveReady:                   {DesignationParkHead, DesignationPCDirector, DesignationCEO},
		AlertVaccinationDriveClosed:                  {DesignationCEO},
		AlertWeighingVerdictRework:                   {DesignationGrowthDirector},
		AlertWeighingSubmitted:                       {DesignationGrowthDirector, DesignationCEO},
		ProofAlertKey("weighing", ProofReworkSuffix): {DesignationParkHead, DesignationGrowthDirector, DesignationCEO},
		ProofAlertKey("counts", ProofPendingSuffix):  {DesignationParkHead, DesignationHealthDirector, DesignationCEO},
		ProofAlertKey("feed", ProofApprovedSuffix):   {DesignationParkHead, DesignationFeedDirector, DesignationCEO},
	}
	for key, defaults := range want {
		alert, ok := AlertByKey(key)
		if !ok {
			t.Fatalf("%s missing", key)
		}
		if !reflect.DeepEqual(alert.DefaultDesignations, defaults) {
			t.Errorf("%s default = %v want %v", key, alert.DefaultDesignations, defaults)
		}
	}
}

func TestDesignationScopeSeparatesParkDesksFromTenantDesks(t *testing.T) {
	for code, want := range map[string]Scope{
		DesignationParkHead: ScopePark, DesignationOperator: ScopePark, DesignationVerifier: ScopeTenant,
		DesignationCEO: ScopeTenant, DesignationPCDirector: ScopeTenant, DesignationProcurementDirector: ScopeTenant,
		"never_seen_before": ScopeTenant,
	} {
		if got := DesignationScope(code); got != want {
			t.Errorf("DesignationScope(%q) = %q want %q", code, got, want)
		}
	}
}

func TestCatalogReturnsACopy(t *testing.T) {
	first := Catalog()
	first[0].Key = "mutated.key"
	if _, ok := AlertByKey("mutated.key"); ok {
		t.Fatal("Catalog() must hand out a copy, not the backing slice")
	}
}
