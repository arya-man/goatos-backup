package domain

import "math/big"

// ---------------------------------------------------------------------------
// Generator inputs
// ---------------------------------------------------------------------------
//
// These are the two INPUTS the generator is a pure function of: a snapshot of the authored
// configuration for one park, and the projected shed grains for one target date. Both are read in
// bounded, batched queries before generation starts. Nothing in this package issues I/O, so the
// whole resolution rule -- including every blocked case -- is testable without a database.

// ShedTag is one entry of the authored tag vocabulary.
//
// AppliesTo is the field the whole kid/adult branch turns on. See the package comment on
// types.go: it, not goats.age_band, is the source of truth.
type ShedTag struct {
	Label     string
	AppliesTo string
}

// FeedItem is one catalog entry, carried in authored display order so every row and every summary
// renders its columns in the same sequence.
type FeedItem struct {
	Label string
	Key   string
}

// RationRate is one currently-open cell of the grid. GramsPerHead is the exact decimal STRING from
// numeric(12,3); it is parsed to a *big.Rat at use, never to a float.
type RationRate struct {
	GramsPerHead string
}

// SessionTemplate is one feeding session: the fraction of the day's quantity it carries, and the
// feed items it actually consists of.
//
// ITEMS IS THE RECIPE, feed_item_catalog IS THE VOCABULARY. This distinction is the whole of
// migration 000005's first half. The catalog lists every feed the TENANT knows about, including the
// four roughages (Hybrid, COFS, Hedge Lucerne, Dry Maize) that are inter-feed SUBSTITUTION
// alternatives -- exactly one of them is ever fed, which is what feed_conversions is for. A
// generator that walks the catalog therefore asks every shed for feeds nobody packs: on one real
// CBE run, 619 kg of Hybrid plus 112 blocked cells of noise.
//
// Items is the source workbook's `Template` tab: the five numbered slots this park+session is
// actually made of, in packing order.
type SessionTemplate struct {
	SessionNo     int32
	Label         string
	SplitFraction string
	// Items are the declared feed slots for this session, in slot_no (packing) order.
	//
	// An EMPTY Items is not "feed everything" and must never be treated as one. It means the park's
	// session template is incomplete, which is a configuration gap and blocks -- see
	// materializeRow. That asymmetry is deliberate and mirrors the rate rule: absence never widens
	// what gets fed.
	Items []FeedItem
}

// ExperimentCell is one hand-authored absolute quantity for an experiment shed.
//
// AbsoluteKg is a SHED TOTAL in kg, already covering every animal in the shed. It is NOT a
// per-head rate and must never be multiplied by head count -- doing so would overfeed the shed by
// a factor of its population. See the experiment planner.
type ExperimentCell struct {
	FeedItemLabel string
	FeedItemKey   string
	AbsoluteKg    string
	Category      string
}

// ConfigSnapshot is everything authored that one park's generation needs, read once.
//
// The maps are keyed by NORMALIZED keys (feed_config_norm form) throughout, so a lookup can never
// miss on a cosmetic spelling difference. The database computes the stored side of those keys as
// GENERATED columns; NormalizeConfigKey computes the live side identically.
type ConfigSnapshot struct {
	ParkID    string
	ParkLabel string
	// ShedTagsByKey maps a normalized tag key to its authored label and course.
	ShedTagsByKey map[string]ShedTag
	// RationGroupByBreedKey maps a normalized ADULT breed key to its ration group label. Kids never
	// consult this map -- they resolve to KidRationGroupLabel directly.
	RationGroupByBreedKey map[string]string
	// FeedItems is the active catalog in authored display order.
	FeedItems []FeedItem
	// RatesByKey is the currently-open grid, keyed by RateKey(group, tag, item). ABSENCE OF AN
	// ENTRY IS THE ONLY ENCODING OF "not configured" and must produce a blocked cell -- there is no
	// sentinel value and no zero default. See the package comment on types.go.
	RatesByKey map[string]RationRate
	// ShedFactorsByKey maps ShedFactorKey(shedID, item) to a multiplier decimal string. A MISSING
	// entry reads as 1.0, which is safe: unlike a missing rate it cannot zero out or invent a
	// ration, it just declines to scale one. Migration 000003 says the same.
	ShedFactorsByKey map[string]string
	// Sessions is the park's active session split, in display order.
	Sessions []SessionTemplate
	// ExperimentByLocation holds the hand-authored absolute quantities keyed by
	// ExperimentLocationKey(shedID, partitionLabel) -- an OPERATIONAL LOCATION, not a shed.
	// A location present here is an experiment; that presence is the whole selection rule for the
	// experiment planner.
	//
	// Keyed by shed alone until 2026-08-07, which could not express the authored data: CBE's
	// Godel 2 has eight partitions of which only Parts 3, 4 and 5 are experiments, so "is this shed
	// an experiment?" had no correct answer. A non-partitioned shed keys on 'whole' and behaves
	// exactly as before.
	ExperimentByLocation map[string][]ExperimentCell
}

// ExperimentLocationKey is the lookup key for ExperimentByLocation: one exact physical shed.
// partitionLabel is accepted for legacy callers but ignored; appending it would split "Castro 2"
// from a stale "2" compatibility label into a second experiment bucket.
func ExperimentLocationKey(shedID, partitionLabel string) string {
	_ = partitionLabel
	return shedID
}

// PartitionGrains is one operational location's grains, carrying the RAW label for display.
type PartitionGrains struct {
	PartitionLabel string
	Grains         []ShedGrain
}

// SplitGrainsByPartition buckets a shed's projected grains into one entry per operational location.
//
// A shed with no partitions returns exactly one entry with an empty label -- identical to the
// pre-2026-08-07 whole-shed input, so a non-partitioned shed's sheet is unchanged byte for byte.
//
// Grouping is on the MATCHING key (so "Part 3" and "part 3" are one partition and cannot be split
// into two rows by an authoring variant), while the entry keeps the FIRST raw label seen for
// display. Order is deterministic: partitions come out in the order they first appear in the
// projection's own stable ordering, never Go map order, or the same request could page differently
// on two runs.
func SplitGrainsByPartition(grains []ShedGrain) []PartitionGrains {
	if len(grains) == 0 {
		// A shed in scope with no projected grains still needs one input: the planners decide what an
		// empty shed produces (a blocked row, or nothing), and dropping it here would silently remove
		// the shed from the sheet instead.
		return []PartitionGrains{{}}
	}
	order := make([]string, 0, 4)
	byKey := make(map[string]*PartitionGrains, 4)
	for _, grain := range grains {
		key := PartitionMatchKey(grain.PartitionLabel)
		entry, seen := byKey[key]
		if !seen {
			entry = &PartitionGrains{PartitionLabel: grain.PartitionLabel}
			byKey[key] = entry
			order = append(order, key)
		}
		entry.Grains = append(entry.Grains, grain)
	}
	out := make([]PartitionGrains, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// PlannedFeedItems is the DISTINCT set of feed items this park declares across all of its
// sessions, in first-declared order (session order, then slot order).
//
// It is the list the grid-driven planner resolves rates for. Doing it once per park rather than
// once per session means a rate lookup is not repeated for an item that appears in both sessions --
// which is every item in the live data, where CBE and CPT declare the same five slots morning and
// evening. The per-SESSION narrowing still happens, later and separately, when a daily row is split
// (see materializeRow), so a park that genuinely feeds different items at different times is
// handled correctly; this is only the union that guarantees every declared item has a computed
// quantity available to select from.
//
// It is also the column order for the page summary and the packing worklist, so an operator's
// columns read in packing-slot order rather than in catalog order.
func (c ConfigSnapshot) PlannedFeedItems() []FeedItem {
	seen := make(map[string]bool)
	out := make([]FeedItem, 0)
	for _, session := range c.Sessions {
		for _, item := range session.Items {
			key := item.Key
			if key == "" {
				key = NormalizeConfigKey(item.Label)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, FeedItem{Label: item.Label, Key: key})
		}
	}
	return out
}

// blockedColumnItems is the item list used to render a row whose failure is at the ROW level (an
// unresolvable tag or ration group, or a park/session that declares no slots at all).
//
// It prefers the declared slots, so a blocked row keeps the same column shape as a resolved one. It
// falls back to the whole catalog ONLY when the park declares nothing, because in that state there
// is no recipe to name the gap with and a row rendered with zero cells is a row an operator's eye
// skips over. That fallback cannot resurrect defect 1: it is reached only on rows that are already
// entirely blocked, so every cell it produces carries a reason and none carries a quantity.
func (c ConfigSnapshot) blockedColumnItems() []FeedItem {
	if planned := c.PlannedFeedItems(); len(planned) > 0 {
		return planned
	}
	return c.FeedItems
}

// RateKey builds the grid lookup key from raw labels, normalizing every component.
func RateKey(rationGroup, shedTag, feedItem string) string {
	return NormalizeConfigKey(rationGroup) + "\x1f" + NormalizeConfigKey(shedTag) + "\x1f" + NormalizeConfigKey(feedItem)
}

// ShedFactorKey builds the per-shed multiplier lookup key. The shed id is a uuid and is compared
// verbatim; only the feed item is normalized.
func ShedFactorKey(shedID, feedItem string) string {
	return shedID + "\x1f" + NormalizeConfigKey(feedItem)
}

// ShedGrain is one projected count grain inside a shed, as delivered by the counts projection.
//
// ManagementStage and Breed are RAW live text off the source sheet, carrying real case and
// separator variants of the same value. They are normalized at lookup, never at read.
type ShedGrain struct {
	ManagementStage string
	Breed           string
	// PartitionLabel is the grain's RAW operational partition ("1", "Part 3"), empty for a shed with
	// no partitions. The counts projection has always returned it; Feed used to drop it here, which
	// is why a shed could only ever be wholly experimental or wholly not.
	PartitionLabel string
	// HeadCount is the PROJECTED head count for the target date: live herd plus the movements that
	// are approved but not yet executed.
	HeadCount int64
	// OverduePending marks a grain whose projected count already assumes a movement that came due
	// before the target date and still has not been executed.
	OverduePending bool
}

// ShedInput is one shed's full set of projected grains. The generator receives every grain for a
// shed together -- never a partial set -- which is why the reader pages by shed.
type ShedInput struct {
	ShedID    string
	ShedLabel string
	// PartitionLabel scopes this input to ONE operational location (shed + partition), which is the
	// unit a planner is selected for. A shed with no partitions has exactly one ShedInput with an
	// empty label; a partitioned shed has one per partition, so Godel 2 can run its authored
	// experiment on Parts 3/4/5 while Parts 1/2/6/7/8 stay on the per-head ration grid.
	//
	// Rows still carry ShedID, so paging by shed keeps every partition of a shed on one page and the
	// "a shed's grains never straddle a page boundary" invariant is unchanged.
	PartitionLabel string
	Grains         []ShedGrain
}

// PartitionKey is the MATCHING token for this input's partition: 'whole' when there is none. It
// mirrors the generated feed_experiment_config.partition_key so an authored cell and a live grain
// meet on the same key. Never render it -- 'whole' is a key, never copy.
func (s ShedInput) PartitionKey() string { return PartitionMatchKey(s.PartitionLabel) }

// PartitionMatchKey is the EXACT Go twin of the migration's generated
// feed_experiment_config.partition_key expression, and the two must be changed together or an
// authored cell stops meeting its live grain.
//
// NULL, "" and a whitespace-only label all mean "not partitioned" and collapse to 'whole', matching
// the operational-location rule. Everything else goes through NormalizeConfigKey, the same
// feed_config_norm twin the rest of this config uses -- so "Part 3", "part 3" and "Part  3" are one
// partition and cannot be split into two by an authoring whitespace variant.
//
// Deliberately NOT the counts projection's partition normalization, which additionally strips a
// leading "part " ("Part 3" -> "3"). That token is for matching inside counts; here BOTH sides of
// the comparison are feed's own (the authored label and the raw grain label), so feed normalizes
// them its own single way. Mixing the two would make "Part 3" and "3" different keys on one side
// and identical on the other.
func PartitionMatchKey(label string) string {
	normalized := NormalizeConfigKey(label)
	if normalized == "" {
		return "whole"
	}
	return normalized
}

// ---------------------------------------------------------------------------
// Intermediate (pre-session) rows
// ---------------------------------------------------------------------------

// DailyItem is one feed item's DAILY quantity for one row, before the session split and before
// rounding.
//
// DailyGrams is an exact *big.Rat and is nil if and only if Blocked is set -- the same
// pointer-is-the-contract rule as ItemQuantity, applied one stage earlier so a blocked cell never
// acquires a number anywhere in the pipeline.
type DailyItem struct {
	FeedItemLabel string
	FeedItemKey   string
	DailyGrams    *big.Rat
	GramsPerHead  *string
	ShedFactor    *string
	Blocked       *BlockedReason
}

// DailyRow is one ration grain's daily plan for one shed, before the session split.
type DailyRow struct {
	ShedTag     string
	Breed       string
	RationGroup string
	// ExperimentArm is the trial group an experiment shed belongs to. Empty on every normal row.
	// It has its OWN field rather than borrowing ShedTag because an arm and a tag are different
	// facts about the shed -- see DirectionRow.ExperimentArm.
	ExperimentArm string
	HeadCount     int64
	// HeadCountInformational is true when the quantities were NOT derived from HeadCount -- the
	// experiment workflow. See DirectionRow.HeadCountInformational.
	HeadCountInformational bool
	OverduePending         bool
	Workflow               string
	Items                  []DailyItem
}
