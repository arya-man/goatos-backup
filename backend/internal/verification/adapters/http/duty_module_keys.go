package http

import "strings"

// navigationModuleForDutyCode translates a position_module_duties module_code into the
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
func navigationModuleForDutyCode(dutyModuleCode string) string {
	normalized := strings.TrimSpace(strings.ToLower(dutyModuleCode))
	switch normalized {
	case "pc.vaccination":
		return "vaccination"
	case "feed.direction":
		return "feed_direction"
	default:
		// Every other duty code is already a navigation key ("weighing", "counts"). Dots are
		// still folded to underscores so a newly added dotted code degrades to the obvious
		// key instead of silently matching nothing.
		return strings.ReplaceAll(normalized, ".", "_")
	}
}
