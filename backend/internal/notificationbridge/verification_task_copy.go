package notificationbridge

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Verification lifecycle pushes are TASK-level, never module-level (maintainer decision
// 2026-09-12). A director reading "Counts proof verified · counts proof for Sumathi 1
// (Coimbatore) is verified." could not tell WHAT was verified: the module name is an internal
// grouping, and the fact on the farm is a pen move, a hoof trimming, a packed bag. Every push
// about a verification item therefore leads with the item's OWN subject sentence -- the one the
// producing module composed for the verifier's queue ("Pen move · Gandhi 2 · from Ho Chi Minh 1 ·
// 1 animal") -- and titles itself with the farm word for the task, resolved from the item's
// CATEGORY (the task type), not its module.
//
// The pending and withdrawn pushes always did this; the approved and closed pushes rebuilt a
// body from module + shed and dropped the subject, which is the defect being closed here.

// verificationTaskNouns maps a verification CATEGORY (the task type a producer registers in
// verificationcatalog) to the word the farm uses for that work. Keyed by category and not by
// module on purpose: "feed" is four different jobs and "counts" is five. A category missing
// here degrades to the module's own word (taskNounFallbacks) rather than to an internal slug;
// TestEveryVerificationCategoryHasATaskNoun keeps the map complete.
var verificationTaskNouns = map[string]string{
	"vaccination_proof":     "Vaccination",
	"weighing_proof":        "Weighing",
	"weighing_fasting":      "Feed & water removal",
	"health_adults":         "Treatment",
	"health_kids":           "Treatment",
	"shifting_move":         "Pen move",
	"pen_reconciliation":    "Pen reconciliation",
	"milk_preparation":      "Milk preparation",
	"milk_feeding":          "Milk feeding",
	"feed_distribution":     "Feeding",
	"feed_packing":          "Feed packing",
	"feed_transport":        "Feed transport",
	"feed_wastage":          "Feed wastage",
	"death_evidence":        "Death record",
	"birth_evidence":        "Birth record",
	"pc_deworming":          "Deworming",
	"pc_anti_protozoan":     "Anti-protozoan",
	"pc_ticks_removal":      "Ticks removal",
	"pc_hoof_trimming":      "Hoof trimming",
	"pc_hair_trimming":      "Hair trimming",
	"pen_visit":             "Pen visit",
	"inventory_vaccine":     "Vaccine stock",
	"pc_feed_water_removal": "Feed & water removal",
}

// taskNounFallbacks is the module-level word used ONLY when a category is unknown, so an
// unregistered category still reads as farm work and never as "counts" or "pc_care".
var taskNounFallbacks = map[string]string{
	legacyVaccinationSourceModule: "Vaccination",
	moduleWeighing:                "Weighing",
	moduleFeed:                    "Feed work",
	modulePCCare:                  "Care work",
	moduleHealth:                  "Treatment",
	moduleCounts:                  "Herd record",
}

// taskNoun returns the farm word for the work a verification item is about.
func taskNoun(category, module string) string {
	if noun, ok := verificationTaskNouns[strings.ToLower(strings.TrimSpace(category))]; ok {
		return noun
	}
	if noun, ok := taskNounFallbacks[strings.ToLower(strings.TrimSpace(module))]; ok {
		return noun
	}
	return "Work"
}

// verificationSubjectLine is the one head every lifecycle push leads with: the item's own
// subject, then the park in parentheses so a director holding two parks can place it without
// opening the app. When a producer sends NO subject (feed transport deliberately does not, because
// its card already renders the shed) the head is the task noun and the pen, so the line still
// names the work and the place rather than "A pen" or a bare module word.
func verificationSubjectLine(subject, noun, shedName, partitionLabel, parkName string) string {
	head := strings.TrimSpace(subject)
	if head == "" {
		head = noun
		if loc := (oploc.OperationalLocation{ShedName: strings.TrimSpace(shedName), PartitionLabel: strings.TrimSpace(partitionLabel)}).Display(); loc != "" {
			head += " · " + loc
		}
	}
	if parkName = strings.TrimSpace(parkName); parkName != "" && !strings.Contains(head, parkName) {
		head += " (" + parkName + ")"
	}
	return head
}

// approvedCopy is the approve push's title and body suffix (maintainer decision 2026-09-12,
// second half): where the verifier's approve is the LAST step, the farm word is "completed", not
// "verified" -- a pen move whose video was accepted is a pen move that is done. Two modules keep
// "verified" because approve is NOT their last step: weighing still has to be CLOSED (its own
// verb, and the weighing consumer already pushes "Weighing pen complete" once every video is in),
// and vaccination has a leadership close after approve that pushes "... closed" to the operator.
// Saying "completed" on those would announce completion twice, the first time falsely.
func approvedCopy(noun, module string) (title, bodySuffix string) {
	switch strings.ToLower(strings.TrimSpace(module)) {
	case legacyVaccinationSourceModule, moduleWeighing:
		return noun + " verified", " — video verified."
	}
	return noun + " completed", " — video verified, work complete."
}
