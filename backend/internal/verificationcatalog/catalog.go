// Package verificationcatalog is the ONE list of registered verification categories.
//
// It exists because the category set is read by TWO processes, not one. The API registers it at
// composition time (bootstrap/api.go); the kernel worker's randomization closeout needs the SAME
// set to know which categories may be settled without a verifier
// (domain.CategoryDefinition.SamplingWaivable, maintainer decision 2026-08-26). A worker holding a
// hand-copied subset would not fail loudly -- it would silently never settle the categories it was
// missing, leaving those producers' records waiting forever on a review that is never coming. One
// list, read twice, cannot drift.
//
// It lives OUTSIDE the verification bounded context on purpose. These definitions name producer
// vocabulary (weighing, counts, feed, pc care, tasks), and verification must keep knowing nothing
// about producers -- so this is composition wiring, a sibling of bootstrap, not part of the module.
//
// Registration ORDER, the producer bridges, and the measurement appliers all stay in
// bootstrap/api.go: this package owns WHAT the categories are, never when they are wired or to
// which bridge. The comments explaining each category's product rules stay at those wiring sites.
package verificationcatalog

import (
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var Vaccination = domain.CategoryDefinition{
	Vertical: "preventive_care", Module: "vaccination", Category: sopbridge.VaccinationVerificationCategory,
	ExpectedMedia: []string{"video"}, MediaLabels: []string{"Vaccination proof video"},
	NavigationModule: "vaccination", NavigationModuleLabel: "Vaccination",
	PageKey: "vaccination", PageLabel: "Vaccination", PageOrder: 1,
}

var Weighing = domain.CategoryDefinition{
	Vertical: weighingdomain.VerificationVerticalWeighing, Module: weighingdomain.VerificationModuleWeighing,
	Category:      weighingdomain.VerificationCategoryWeighing,
	ExpectedMedia: []string{"video"}, MediaLabels: []string{"Weighing video"},
	// THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17). Weighing is
	// the one category today whose proof shows a number an operator typed, so it is
	// the one that declares a correctable measurement. Every visible word lives here
	// because the backend owns labels: her phone and her admin-web drawer render this
	// same copy, and neither may word it itself.
	//
	// The help sentence says REPLACES on purpose. A verifier who believes she is
	// filing a note rather than overwriting the operator's record is the single most
	// expensive misunderstanding this control can cause.
	MeasurementCorrection: &domain.MeasurementCorrectionSpec{
		Title:       "Correct the weight",
		Help:        "Enter the weight you can see in the video. It replaces the weight recorded here.",
		ValueLabel:  "Corrected weight (kg)",
		SubmitLabel: "Save corrected weight",
		// NO CountLabel / CountRefTypes, deliberately (maintainer decision
		// 2026-08-24, retiring the 2026-08-17 head-count edit): the lump-sum
		// head count is snapshotted from the herd register at submit and is
		// frozen, so NOBODY — verifier included — may change it. Both clients
		// render the count input only when this spec carries a CountLabel, so
		// omitting it here removes the field from the phone and the admin-web
		// drawer alike, and verification's own validateMeasurement refuses any
		// count a stale client still sends (measurement_count_not_supported).
	},
	NavigationModule: "weighing", NavigationModuleLabel: "Weighing",
	PageKey: "weighing", PageLabel: "Weighing", PageOrder: 1,
}

// WeighingFasting is the feed & water removal precondition proof (maintainer
// decision 2026-09-03): two live-camera videos — feed removed, water removed —
// recorded the evening before a weigh date, reviewed post-hoc. Its own page
// under the Weighing module so removal footage never appears under a weight
// label.
var WeighingFasting = domain.CategoryDefinition{
	Vertical: weighingdomain.VerificationVerticalWeighing, Module: weighingdomain.VerificationModuleWeighing,
	Category:         weighingdomain.VerificationCategoryFasting,
	ExpectedMedia:    []string{"video", "video"},
	MediaLabels:      []string{"Feed removal video", "Water removal video"},
	NavigationModule: "weighing", NavigationModuleLabel: "Weighing",
	PageKey: "weighing_fasting", PageLabel: "Feed & Water Removal", PageOrder: 2,
}

var HealthAdults = domain.CategoryDefinition{
	Vertical: "health", Module: "health", Category: "health_adults",
	ExpectedMedia: []string{"video"}, MediaLabels: []string{"Health case video"},
	NavigationModule: "aas_health", NavigationModuleLabel: "Health",
	PageKey: "health_adults", PageLabel: "Adults", PageOrder: 1,
}

var HealthKids = domain.CategoryDefinition{
	Vertical: "health", Module: "health", Category: "health_kids",
	ExpectedMedia: []string{"video"}, MediaLabels: []string{"Health case video"},
	NavigationModule: "aas_health", NavigationModuleLabel: "Health",
	PageKey: "health_kids", PageLabel: "Kids", PageOrder: 2,
}

var Shifting = domain.CategoryDefinition{
	Vertical: countsdomain.VerificationVerticalShifting, Module: countsdomain.VerificationModuleShifting,
	Category: countsdomain.VerificationCategoryShifting, ExpectedMedia: []string{"video"},
	MediaLabels:      []string{"Shifting video"},
	NavigationModule: "counts", NavigationModuleLabel: "Counts",
	PageKey: "shifting", PageLabel: "Shifting", PageOrder: 3,
}

var PenReconciliation = domain.CategoryDefinition{
	Vertical: countsdomain.VerificationVerticalPenReconciliation, Module: countsdomain.VerificationModulePenReconciliation,
	Category: countsdomain.VerificationCategoryPenReconciliation, ExpectedMedia: []string{"video"},
	MediaLabels: []string{"Pen return video"},
	// Reviewed under Counts beside shifting/birth/death: a wrong-pen return is herd-operations
	// field work, and the verifier's verdict is the ONLY gate (no approver, maintainer decision
	// 2026-09-02). The register is truth and the verdict consumer never rewrites it.
	NavigationModule: "counts", NavigationModuleLabel: "Counts",
	PageKey: "pen_reconciliation", PageLabel: "Reconcile", PageOrder: 4,
}

var MilkPreparation = domain.CategoryDefinition{
	Vertical: countsdomain.VerificationVerticalMilkPreparation, Module: countsdomain.VerificationModuleMilkPreparation,
	Category:      countsdomain.VerificationCategoryMilkPreparation,
	ExpectedMedia: []string{"video", "video", "video", "video", "video"},
	// Reviewed under MILK, not Counts (maintainer decision 2026-08-09). The two milk tasks were
	// split out of Counts into their own operator module on 2026-07-31, but their VERIFICATION
	// was deliberately left in the Counts lens -- so a verifier saw Milk Prep and Milk Feeding
	// filed under Herd Operations, while the Milk module in her own drawer pointed at an
	// invented "milk_proof" category that no producer writes and that answers 400 forever.
	// Review now follows the module the work belongs to.
	SLAHours: 24, NavigationModule: "milk", NavigationModuleLabel: "Milk",
	PageKey: "milk_preparation", PageLabel: "Milk Prep", PageOrder: 1,
}

var MilkFeeding = domain.CategoryDefinition{
	Vertical: countsdomain.VerificationVerticalMilkFeeding, Module: countsdomain.VerificationModuleMilkFeeding,
	Category: countsdomain.VerificationCategoryMilkFeeding, ExpectedMedia: []string{"video", "video"},
	MediaLabels: []string{"Milk preparation video", "Milk feeding video"}, SLAHours: 24,
	NavigationModule: "milk", NavigationModuleLabel: "Milk",
	PageKey: "milk_feeding", PageLabel: "Milk Feeding", PageOrder: 2,
}

var FeedDistribution = domain.CategoryDefinition{
	Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
	Category:         feeddirectiondomain.VerificationCategoryFeed,
	ExpectedMedia:    []string{"photo", "video", "video"},
	MediaLabels:      []string{"Feed weight photo", "Feed distribution video", "Water distribution video"},
	NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
	PageKey: "feed_distribution", PageLabel: "Feed Distribution", PageOrder: 1,
}

var FeedPacking = domain.CategoryDefinition{
	Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
	Category:      feeddirectiondomain.VerificationCategoryPacking,
	ExpectedMedia: []string{"video"},
	MediaLabels:   []string{"Feed packing video"},
	MeasurementCorrection: &domain.MeasurementCorrectionSpec{
		Title:       "Record the packed quantities",
		Help:        "Watch the video and enter the packed weight you can see for each feed item. Your readings become the recorded packed quantities.",
		ValueLabel:  "Packed quantity (kg)",
		SubmitLabel: "Save packed quantities",
		// The numbers are BORN on her screen -- the operator sends a video and nothing else --
		// so every box must be filled before the approve lands. An unreadable video is a
		// rejection, never a guess.
		RequiredForApprove: true,
		// One value PER FEED ITEM, with the field list on each item; an item enqueued with no
		// fields (frozen sheet unreadable at submit) degrades to a judge-the-video approve.
		PerItemFields: true,
	},
	NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
	PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2,
}

var FeedTransport = domain.CategoryDefinition{
	Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
	Category: feeddirectiondomain.VerificationCategoryTransport, ExpectedMedia: []string{"video"},
	MediaLabels:      []string{"Feed transport video"},
	NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
	PageKey: "feed_transport", PageLabel: "Feed Transport", PageOrder: 3,
}

var FeedWastage = domain.CategoryDefinition{
	Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
	Category:      feeddirectiondomain.VerificationCategoryWastage,
	ExpectedMedia: []string{"video"},
	MediaLabels:   []string{"Feed wastage video"},
	MeasurementCorrection: &domain.MeasurementCorrectionSpec{
		Title:       "Record the wastage",
		Help:        "Enter the leftover feed weight you can see in the video. It replaces any wastage weight recorded here.",
		ValueLabel:  "Measured wastage (kg)",
		SubmitLabel: "Save wastage weight",
		// The number is BORN here: the operator sends a video and nothing else, so approving
		// without a reading would complete a pen-day with no wastage at all. An unreadable
		// value is a rejection, never a guess.
		RequiredForApprove: true,
	},
	NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
	PageKey: "feed_wastage", PageLabel: "Feed Wastage", PageOrder: 4,
}

var DeathEvidence = domain.CategoryDefinition{
	Vertical: tasksdomain.VerificationVerticalCounts, Module: tasksdomain.VerificationModuleCounts,
	Category:      tasksdomain.VerificationCategoryDeathEvidence,
	ExpectedMedia: []string{"video", "video"},
	SLAHours:      24, NavigationModule: "counts", NavigationModuleLabel: "Counts",
	PageKey: "death", PageLabel: "Death", PageOrder: 2,
}

var BirthEvidence = domain.CategoryDefinition{
	Vertical: tasksdomain.VerificationVerticalCounts, Module: tasksdomain.VerificationModuleCounts,
	Category:      tasksdomain.VerificationCategoryBirthEvidence,
	ExpectedMedia: []string{"video"},
	SLAHours:      24, NavigationModule: "counts", NavigationModuleLabel: "Counts",
	PageKey: "birth", PageLabel: "Birth", PageOrder: 1,
}

// PCCare returns the four PC Care categories -- one per work category, all sharing the pc_care
// navigation module so the verifier gets ONE Verify tab and the categories split as page filters.
func PCCare() []domain.CategoryDefinition {
	out := make([]domain.CategoryDefinition, 0, len(pccaredomain.Categories))
	for order, workCategory := range pccaredomain.Categories {
		out = append(out, domain.CategoryDefinition{
			Vertical: pccaredomain.VerificationVerticalPreventiveCare, Module: pccaredomain.VerificationModulePCCare,
			Category: pccaredomain.VerificationCategoryFor(workCategory),
			// The media set is DYNAMIC (animals x slots), so no positional ExpectedMedia /
			// MediaLabels contract: every clip is a video and carries its animal's tag, the
			// operator, and the capture time burned into its overlay.
			NavigationModule: "pc_care", NavigationModuleLabel: "Preventive Care",
			PageKey:   pccaredomain.VerificationCategoryFor(workCategory),
			PageLabel: pccaredomain.CategoryLabel(workCategory), PageOrder: order + 1,
		})
	}
	return out
}

// All is every registered category, in no significant order. It is what a process that needs the
// WHOLE set (the randomization closeout) reads, so a category added above joins it automatically.
func All() []domain.CategoryDefinition {
	out := []domain.CategoryDefinition{
		Vaccination,
		Weighing,
		WeighingFasting,
		HealthAdults,
		HealthKids,
		Shifting,
		PenReconciliation,
		MilkPreparation,
		MilkFeeding,
		FeedDistribution,
		FeedPacking,
		FeedTransport,
		FeedWastage,
		DeathEvidence,
		BirthEvidence,
	}
	return append(out, PCCare()...)
}
