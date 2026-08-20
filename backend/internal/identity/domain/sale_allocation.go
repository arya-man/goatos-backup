package domain

import "strings"

// Sale allocation: which real animals a recorded sale is made of.
//
// The flow this serves is pick -> review -> confirm. A person filters to a park, a
// shed and its pens, multi-selects animals across as many sheds as the sale spans,
// reviews the picked identifiers shed-wise, and confirms; confirming records the
// allocation and exits each animal as sold through the canonical per-goat transition.
//
// WHY PREVIEW AND CONFIRM ARE SEPARATE WRITES-THAT-AREN'T. Preview mutates NOTHING.
// It exists because selling an animal is irreversible in the world -- the animal
// physically leaves -- and a person is owed the chance to read back the exact list
// before it happens. Collapsing the two into one call would remove the only moment
// the operator can catch a mis-tap, which is the whole point of the review step.

// SaleBlocker is why one animal may not be sold right now.
//
// THIS IS A MEDICAL AND BIOSECURITY GATE, NOT A UI CONVENIENCE. An animal in
// quarantine or ICU must not leave the farm: quarantine exists because the animal may
// be carrying something contagious, and selling it moves that risk to another herd. A
// milk-drinking kid is not a saleable animal. An animal inside a medicine withdrawal
// period carries drug residue and must not enter the food chain until the period
// expires. Each is named in docs/features/critical-animal-action-guardrails.md as a
// sale/allocation blocker.
//
// The gate is FAIL-CLOSED (maintainer decision 2026-08-20): a blocked animal cannot be
// confirmed, and there is deliberately no override parameter on the confirm route. If a
// blocker is wrong, the fix is to resolve the underlying state -- release the
// quarantine, close the treatment, wait out the withdrawal -- never to add a path
// around this. An override would put a contagious or residue-carrying animal on a
// buyer's truck on one person's say-so.
type SaleBlocker string

const (
	// BlockerNone means the animal may be sold.
	BlockerNone SaleBlocker = ""
	// BlockerClinical covers quarantine, ICU, sick and under-treatment lifecycle states.
	BlockerClinical SaleBlocker = "clinical_state"
	// BlockerMilkKid is a kid still on milk.
	BlockerMilkKid SaleBlocker = "milk_drinking_kid"
	// BlockerWithdrawal is an unexpired medicine withdrawal period.
	BlockerWithdrawal SaleBlocker = "medicine_withdrawal"
	// BlockerAlreadyExited is an animal that already left the active herd -- sold,
	// dead, culled, transferred, lost. Not a medical blocker; a "this is not a live
	// animal" one, kept in the same vocabulary so the picker has one reason field.
	BlockerAlreadyExited SaleBlocker = "already_exited"
	// BlockerAlreadyTagged is an animal already tagged to another live sale. One
	// animal cannot be sold to two buyers.
	BlockerAlreadyTagged SaleBlocker = "already_tagged"
)

// clinicalLifecycleStates are the lifecycle_status values that block a sale.
//
// These are LIFECYCLE values, not a separate health column: goats_lifecycle_status_check
// admits 'quarantine', 'icu', 'sick' and 'under_treatment' alongside 'alive'. That is why
// a generic "is the animal still in the herd" check passes them -- they are all live
// animals -- and why this set has to be named explicitly.
var clinicalLifecycleStates = map[string]bool{
	"quarantine":      true,
	"icu":             true,
	"sick":            true,
	"under_treatment": true,
}

// exitedLifecycleStates are the values meaning the animal already left the active herd.
var exitedLifecycleStates = map[string]bool{
	"dead": true, "sold": true, "culled": true,
	"transferred": true, "lost": true, "merged": true, "inactive": true,
}

// milkDrinkingStages are the management stages of a kid still on milk.
//
// K1/K2/K3 are the cohorts the milk-preparation module directs milk for
// (counts/domain.milkPreparationRuleForStage), K3 being the seven-day WEANING window.
// A kid past K3 is weaned and is not blocked here.
//
// K0 IS INCLUDED EVEN THOUGH MILK PREPARATION DOES NOT DIRECT FOR IT. K0 sits below K1
// in the stage ladder (counts/domain shifting vocabulary) -- a newborn, younger than the
// youngest cohort the milk matrix covers. Reading its absence from that matrix as "not a
// milk kid" would invert the fact: it is absent because it is on its mother, not because
// it is weaned. Leaving it out would make the very youngest animal on the farm the one
// animal this gate does not protect.
var milkDrinkingStages = map[string]bool{
	"k0": true, "k1": true, "k2": true, "k3": true,
}

// IsClinicalSaleState reports whether a lifecycle status blocks a sale on clinical
// grounds. Exported because the picker read and the confirm gate must agree, and two
// copies of this set is how they would come to disagree.
func IsClinicalSaleState(lifecycleStatus string) bool {
	return clinicalLifecycleStates[strings.ToLower(strings.TrimSpace(lifecycleStatus))]
}

// IsExitedLifecycle reports whether the animal already left the active herd.
func IsExitedLifecycle(lifecycleStatus string) bool {
	return exitedLifecycleStates[strings.ToLower(strings.TrimSpace(lifecycleStatus))]
}

// IsMilkDrinkingStage reports whether a management stage is a kid still on milk.
func IsMilkDrinkingStage(managementStage string) bool {
	return milkDrinkingStages[strings.ToLower(strings.TrimSpace(managementStage))]
}

// SaleCandidateState is everything the gate needs to judge ONE animal. It is filled by
// the read adapter in a single set-based query and is the ONLY input to ResolveSaleBlocker,
// so the picker list and the confirm gate cannot reach different verdicts about the same
// animal -- they call the same function over the same shape.
type SaleCandidateState struct {
	GoatID          string
	Exists          bool
	Merged          bool
	LifecycleStatus string
	ManagementStage string
	// WithdrawalUntil is the latest medicine withdrawal date recorded against this
	// animal, as an Asia/Kolkata business date in YYYY-MM-DD form. Empty when none.
	WithdrawalUntil string
	// TaggedToOtherDealID is a live sale this animal is already tagged to, if any.
	TaggedToOtherDealID string
	RowVersion          int
}

// SaleBlockerReason pairs the machine code with the sentence a person reads.
type SaleBlockerReason struct {
	Blocker SaleBlocker
	// Reason is FARM COPY, composed here so every surface renders the same sentence
	// verbatim. Per the user-facing copy firewall it names the animal's situation, never
	// the column or state machine behind it: "In quarantine", not "lifecycle_status=quarantine".
	Reason string
}

// Sellable reports whether the animal may be confirmed onto a sale.
func (r SaleBlockerReason) Sellable() bool { return r.Blocker == BlockerNone }

// ResolveSaleBlocker is THE sale gate. One implementation, called by both the picker read
// and the confirm path.
//
// ORDER MATTERS AND IS NOT ARBITRARY. Existence and identity come first because a
// merged or missing animal has no state worth judging. "Already exited" comes before the
// medical blockers because an animal that already left is not a live animal and saying
// "In quarantine" about it would be false. The three medical blockers then run in
// severity order, so an animal that is BOTH in quarantine and inside a withdrawal period
// reports the quarantine -- the more serious fact, and the one that must be resolved first.
//
// businessToday is the Asia/Kolkata business date (YYYY-MM-DD). It is passed in rather
// than read from the clock here so the gate is deterministic and testable, and so a
// withdrawal that expires today is judged against the farm's day, never UTC's.
func ResolveSaleBlocker(state SaleCandidateState, businessToday string) SaleBlockerReason {
	if !state.Exists {
		return SaleBlockerReason{Blocker: BlockerAlreadyExited, Reason: "This animal is not in the herd"}
	}
	if state.Merged {
		return SaleBlockerReason{Blocker: BlockerAlreadyExited, Reason: "This animal's record was merged into another"}
	}
	if IsExitedLifecycle(state.LifecycleStatus) {
		return SaleBlockerReason{Blocker: BlockerAlreadyExited, Reason: exitedReason(state.LifecycleStatus)}
	}
	if state.TaggedToOtherDealID != "" {
		return SaleBlockerReason{Blocker: BlockerAlreadyTagged, Reason: "Already tagged to another sale"}
	}
	if IsClinicalSaleState(state.LifecycleStatus) {
		return SaleBlockerReason{Blocker: BlockerClinical, Reason: clinicalReason(state.LifecycleStatus)}
	}
	if IsMilkDrinkingStage(state.ManagementStage) {
		return SaleBlockerReason{Blocker: BlockerMilkKid, Reason: "Still a milk-drinking kid"}
	}
	// String comparison is correct for YYYY-MM-DD and avoids a timezone round trip: the
	// two sides are already the same business-date form. An animal is clear ON the
	// withdrawal date itself, so this blocks only while today is strictly before it.
	if state.WithdrawalUntil != "" && businessToday < state.WithdrawalUntil {
		return SaleBlockerReason{Blocker: BlockerWithdrawal, Reason: "Medicine withdrawal until " + state.WithdrawalUntil}
	}
	return SaleBlockerReason{Blocker: BlockerNone}
}

// clinicalReason names the animal's situation in farm words.
func clinicalReason(lifecycleStatus string) string {
	switch strings.ToLower(strings.TrimSpace(lifecycleStatus)) {
	case "quarantine":
		return "In quarantine"
	case "icu":
		return "In ICU"
	case "sick":
		return "Currently sick"
	case "under_treatment":
		return "Under treatment"
	default:
		return "Not well enough to sell"
	}
}

// exitedReason says which way the animal already left, because "already gone" and
// "already sold to someone else" call for different next steps by the person reading it.
func exitedReason(lifecycleStatus string) string {
	switch strings.ToLower(strings.TrimSpace(lifecycleStatus)) {
	case "sold":
		return "Already sold"
	case "dead":
		return "Recorded as dead"
	case "culled":
		return "Already culled"
	case "transferred":
		return "Already transferred out"
	case "lost":
		return "Recorded as lost"
	default:
		return "No longer in the active herd"
	}
}
