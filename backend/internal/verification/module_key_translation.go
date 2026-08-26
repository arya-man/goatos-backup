package verification

import "strings"

// NavigationModuleForDutyCode translates a position_module_duties module_code into the
// NavigationModule key the verification category registry uses.
//
// The two vocabularies drifted: duties are seeded as "pc.vaccination" and "feed.direction"
// (dotted, product-area shaped), while registered categories carry "vaccination" and
// "feed_direction". "weighing" and "counts" happen to be spelled identically in both, which is
// why weighing verification worked and hid the bug -- a verifier holding a pc.vaccination verify
// duty, with Vaccination showing in their drawer, was refused the vaccination queue outright
// ("verifier is not assigned to this module") and could never review a vaccination proof.
//
// Translating at the gate keeps both vocabularies intact: the seeder still speaks module codes
// and the registry still speaks navigation keys.
func NavigationModuleForDutyCode(dutyModuleCode string) string {
	normalized := strings.TrimSpace(strings.ToLower(dutyModuleCode))
	// "preventive_care" is the LEGACY vaccination duty code; workforce's roster repository still
	// treats it as equivalent to "pc.vaccination"/"vaccination" when resolving duty holders, so a
	// row seeded before the current convention must translate here too or its holder is refused
	// the vaccination queue -- the same failure this translation exists to remove.
	if normalized == "preventive_care" {
		return "vaccination"
	}
	switch normalized {
	case "feed.direction":
		return "feed_direction"
	case "pc.care", "pc_care":
		return "pc_care"
	}
	// Generic: strip a "pc." product-area prefix and fold dots, matching
	// workforce/app.normalizeModuleFeatureKey so the two do not drift. "pc.vaccination" ->
	// "vaccination"; "weighing"/"counts" are already navigation keys and pass through.
	normalized = strings.TrimPrefix(normalized, "pc.")
	return strings.ReplaceAll(normalized, ".", "_")
}
