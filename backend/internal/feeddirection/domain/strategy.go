package domain

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Quantity strategies
// ---------------------------------------------------------------------------
//
// A shed's DAILY quantities are derived by a pluggable ShedPlanner. Everything that happens after
// that -- the session split, the rounding, the blocked-vs-zero encoding, the totals, the packing
// rollup -- is SHARED and lives in generate.go, so a new workflow cannot accidentally reinvent
// (or quietly diverge from) the safety rules.
//
// This is deliberately a strategy seam rather than a branch. Experiment sheds compute quantity on
// a completely different basis from normal sheds -- absolute hand-authored kg per shed instead of
// head count x grams per head x shed factor -- and scattering `if experiment { ... }` through the
// generator would mean every future change to session splitting or rounding had to be made twice
// and kept in sync. Adding a third workflow means writing one ShedPlanner and adding it to the
// list in NewPlannerSet; nothing in the pipeline changes.

// ShedPlanner derives one shed's DAILY (pre-session) quantities.
//
// A planner never rounds, never splits by session, and never decides how a blocked cell is
// rendered. It answers exactly one question: for this shed, what are the ration grains and how
// many grams per day does each get of each feed item.
type ShedPlanner interface {
	// Workflow is the label stamped on every row this planner produces.
	Workflow() string
	// Applies reports whether this planner owns the shed. The planner set consults planners in
	// order and the FIRST match wins, so a specific planner (experiment) must be registered ahead
	// of the general fallback (normal).
	Applies(shed ShedInput, cfg ConfigSnapshot) bool
	// PlanDaily returns the shed's daily rows. It must return one row per ration grain, with one
	// item entry per feed item it is responsible for, and must mark an item BLOCKED rather than
	// returning a zero whenever the configuration needed to derive it is absent.
	PlanDaily(shed ShedInput, cfg ConfigSnapshot) []DailyRow

	// SessionFeedItems returns the items this planner's rows carry in ONE session, in the order
	// they must be packed.
	//
	// Item selection lives on the planner rather than in the shared pipeline because the two
	// workflows answer "what is this shed fed" from genuinely different sources -- the park's
	// authored session slots versus the shed's own hand-entered cells -- and a
	// `if experiment { ... }` in generate.go would put that answer in the one place strategy.go
	// exists to keep it out of.
	//
	// `scoped` reports whether this planner scopes its items by session at all. False means the
	// row's own items are emitted as planned -- the experiment contract. It is a SEPARATE return
	// value rather than a nil/empty slice convention on purpose: "this planner does not filter" and
	// "this session declares nothing, so block" are opposite outcomes, and a nil slice arriving from
	// an un-populated field must never be able to mean the permissive one.
	SessionFeedItems(cfg ConfigSnapshot, session SessionTemplate) (items []FeedItem, scoped bool)
}

// PlannerSet is the ordered strategy registry.
type PlannerSet struct {
	planners []ShedPlanner
}

// NewPlannerSet returns the planner set in force.
//
// ORDER IS THE SELECTION RULE. ExperimentPlanner is first because its Applies is specific (the
// shed has hand-authored rows); NormalPlanner is last because its Applies always matches and it is
// the fallback. A third workflow slots in ahead of NormalPlanner with its own specific Applies.
func NewPlannerSet() PlannerSet {
	return PlannerSet{planners: []ShedPlanner{
		ExperimentPlanner{},
		NormalPlanner{},
	}}
}

// PlannerFor returns the planner that owns a shed.
//
// It cannot return nil: NormalPlanner matches everything, so the set is total by construction. A
// shed can therefore never fall through the strategy selection and silently produce no rows at
// all -- which would look identical to an empty shed on the feed sheet.
func (s PlannerSet) PlannerFor(shed ShedInput, cfg ConfigSnapshot) ShedPlanner {
	for _, p := range s.planners {
		if p.Applies(shed, cfg) {
			return p
		}
	}
	return NormalPlanner{}
}

// ---------------------------------------------------------------------------
// Normal workflow: the ration grid
// ---------------------------------------------------------------------------

// NormalPlanner resolves quantities from the authored ration grid:
//
//	daily grams = projected_head_count x grams_per_head x COALESCE(shed_factor, 1.0)
//
// It is the fallback planner and matches every shed.
type NormalPlanner struct{}

func (NormalPlanner) Workflow() string { return WorkflowNormal }

// Applies always returns true: this is the general case.
func (NormalPlanner) Applies(ShedInput, ConfigSnapshot) bool { return true }

// SessionFeedItems returns the session's authored slots, in packing order.
//
// The park's `Template` declaration is the complete answer to "what does this session consist of",
// so a catalog item that was never declared is simply not asked for. That is a narrowing of the
// QUESTION and not a softening of the blocked-vs-zero ANSWER: a slot that IS declared and has no
// authored rate still blocks in normalItem below, exactly as it always did.
func (NormalPlanner) SessionFeedItems(_ ConfigSnapshot, session SessionTemplate) ([]FeedItem, bool) {
	return session.Items, true
}

// PlanDaily groups the shed's projected grains by (shed tag, breed) and resolves each group's
// ration.
//
// SEX IS SUMMED AWAY. The counts projection arrives at (stage, breed, sex) grain, but sex is not
// part of the ration lookup key, so keeping it would split one feeding instruction into two
// half-sized rows an operator has to re-add by hand. Raw stage variants that normalize to the same
// authored tag ('ICU- kid' and 'ICU-Kid') collapse into one row for the same reason.
//
// BREED IS NOT summed away even when two breeds share a ration group. Beetal and Sirohi both
// resolve to 'Beetal/Sirohi', and the operator needs to see which animals are in the shed;
// quantity is linear in head count, so keeping them as separate rows produces the identical total.
func (NormalPlanner) PlanDaily(shed ShedInput, cfg ConfigSnapshot) []DailyRow {
	type groupKey struct{ tagKey, breedKey string }
	type accumulator struct {
		order          int
		rawStage       string
		rawBreed       string
		headCount      int64
		overduePending bool
	}

	groups := map[groupKey]*accumulator{}
	order := 0
	for _, grain := range shed.Grains {
		key := groupKey{
			tagKey:   NormalizeConfigKey(grain.ManagementStage),
			breedKey: NormalizeConfigKey(grain.Breed),
		}
		acc, ok := groups[key]
		if !ok {
			acc = &accumulator{order: order, rawStage: grain.ManagementStage, rawBreed: grain.Breed}
			groups[key] = acc
			order++
		}
		acc.headCount += grain.HeadCount
		acc.overduePending = acc.overduePending || grain.OverduePending
	}

	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	// Deterministic output order. A feed sheet that reorders its rows between two requests for the
	// same day is unusable as a printed instruction.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tagKey != keys[j].tagKey {
			return keys[i].tagKey < keys[j].tagKey
		}
		return keys[i].breedKey < keys[j].breedKey
	})

	rows := make([]DailyRow, 0, len(keys))
	for _, key := range keys {
		acc := groups[key]
		rows = append(rows, normalRow(shed, cfg, key.tagKey, key.breedKey, acc.rawStage, acc.rawBreed, acc.headCount, acc.overduePending))
	}
	return rows
}

// normalRow resolves ONE ration grain: the kid/adult branch, the ration group, and then every feed
// item's rate.
func normalRow(
	shed ShedInput,
	cfg ConfigSnapshot,
	tagKey, breedKey, rawStage, rawBreed string,
	headCount int64,
	overduePending bool,
) DailyRow {
	row := DailyRow{
		ShedTag:        rawStage,
		Breed:          rawBreed,
		HeadCount:      headCount,
		OverduePending: overduePending,
		Workflow:       WorkflowNormal,
	}

	// STEP 1 -- the shed tag. Everything else depends on it, including which course the animals are
	// on, so an unresolvable tag blocks the whole row rather than defaulting to either course.
	// Guessing here would feed a kid an adult ration.
	tag, ok := cfg.ShedTagsByKey[tagKey]
	if !ok {
		return blockedRow(row, cfg, BlockedReason{
			Code: BlockReasonUnknownShedTag,
			Detail: fmt.Sprintf(
				"management stage %q (normalized %q) is not in the authored feed shed-tag vocabulary; no ration course can be selected",
				rawStage, tagKey),
		})
	}
	// Report the AUTHORED label, not the raw source text: the raw text carries cosmetic variants
	// ('ICU- kid'), and a feed sheet should show the canonical tag it actually resolved to.
	row.ShedTag = tag.Label

	// STEP 2 -- the ration group, via the kid/adult branch.
	//
	// THE BRANCH IS applies_to, NOT goats.age_band. See the package comment on types.go: age_band
	// contradicts the tag on 6 live animals, and the tag is what the grid is indexed by. A kid of
	// ANY breed resolves to the single 'Kid' group and never consults the breed map -- the source
	// workbook stores the literal string 'Kid' in its breed column.
	if tag.AppliesTo == AppliesToKid {
		row.RationGroup = KidRationGroupLabel
	} else {
		group, ok := cfg.RationGroupByBreedKey[breedKey]
		if !ok {
			return blockedRow(row, cfg, BlockedReason{
				Code: BlockReasonUnknownRationGroup,
				Detail: fmt.Sprintf(
					"adult breed %q (normalized %q) has no ration-group mapping; tag %q is on the adult course",
					rawBreed, breedKey, tag.Label),
			})
		}
		row.RationGroup = group
	}

	// STEP 3 -- one rate lookup per DECLARED feed item.
	//
	// PlannedFeedItems, not cfg.FeedItems. The catalog is the tenant's feed vocabulary; the park's
	// session slots are its recipe. Walking the catalog here is defect 1 of migration 000005: it
	// asked every shed for the four substitution roughages that are alternatives to one another,
	// producing quantities nobody packs and blocked cells for feeds the park never intended to
	// serve.
	planned := cfg.PlannedFeedItems()
	row.Items = make([]DailyItem, 0, len(planned))
	for _, item := range planned {
		row.Items = append(row.Items, normalItem(shed, cfg, row.RationGroup, tag.Label, item, headCount))
	}
	return row
}

// normalItem resolves one (group, tag, item) cell.
//
// THIS IS WHERE THE ZERO-VS-MISSING RULE IS ENFORCED. A missing map entry produces a blocked item
// with no number. An authored 0 produces a fully numeric resolved item. The two are never
// collapsed, and there is no COALESCE, no default, and no fallback to another park's rate.
func normalItem(
	shed ShedInput,
	cfg ConfigSnapshot,
	rationGroup, shedTag string,
	item FeedItem,
	headCount int64,
) DailyItem {
	out := DailyItem{FeedItemLabel: item.Label, FeedItemKey: item.Key}

	rate, ok := cfg.RatesByKey[RateKey(rationGroup, shedTag, item.Label)]
	if !ok {
		out.Blocked = &BlockedReason{
			Code: BlockReasonNoRationRate,
			Detail: fmt.Sprintf(
				"no currently-open ration rate for park %s / group %q / tag %q / item %q",
				cfg.ParkLabel, rationGroup, shedTag, item.Label),
		}
		return out
	}
	gramsPerHead, ok := ParseDecimal(rate.GramsPerHead)
	if !ok {
		// An unparseable stored rate is a data fault, not a zero. Blocking surfaces it; defaulting
		// would feed the shed nothing while looking correct.
		out.Blocked = &BlockedReason{
			Code: BlockReasonNoRationRate,
			Detail: fmt.Sprintf(
				"stored ration rate %q for group %q / tag %q / item %q is not a valid decimal",
				rate.GramsPerHead, rationGroup, shedTag, item.Label),
		}
		return out
	}

	// A MISSING shed factor reads as 1.0, and that asymmetry with the rate above is deliberate:
	// declining to scale a ration is safe, whereas inventing one is not. A factor of 0 must be
	// authored explicitly. Migration 000003 states the same rule.
	//
	// A PRESENT-but-unparseable factor is a different case entirely, and must mirror the rate
	// branch above rather than silently falling through to 1.0: an authored row exists, so "decline
	// to scale" is no longer a safe default -- something was written that this code cannot read,
	// and defaulting to 1.0 would apply an UNAUTHORED multiplier while looking like a deliberate
	// "no factor configured" choice. Block the cell instead (P2-FACTOR).
	factorLabel := "1.0000"
	factor := new(big.Rat).SetInt64(1)
	if raw, ok := cfg.ShedFactorsByKey[ShedFactorKey(shed.ShedID, item.Label)]; ok {
		parsed, parseOK := ParseDecimal(raw)
		if !parseOK {
			out.Blocked = &BlockedReason{
				Code: BlockReasonInvalidShedFactor,
				Detail: fmt.Sprintf(
					"stored shed factor %q for shed %s / item %q is not a valid decimal",
					raw, shed.ShedID, item.Label),
			}
			return out
		}
		factor = parsed
		factorLabel = raw
	}

	daily := new(big.Rat).SetInt64(headCount)
	daily.Mul(daily, gramsPerHead)
	daily.Mul(daily, factor)

	gramsPerHeadLabel := rate.GramsPerHead
	out.DailyGrams = daily
	out.GramsPerHead = &gramsPerHeadLabel
	out.ShedFactor = &factorLabel
	return out
}

// blockedRow marks EVERY feed item on a row blocked for one shared reason -- used when the failure
// is at the grain level (unresolvable tag or ration group) rather than at a single cell.
//
// It emits one blocked item per catalog entry rather than a single row-level flag so the row keeps
// the same column shape as a resolved row. A feed sheet whose blocked rows have no cells would
// render as a short row an operator's eye skips over.
func blockedRow(row DailyRow, cfg ConfigSnapshot, reason BlockedReason) DailyRow {
	columns := cfg.blockedColumnItems()
	row.Items = make([]DailyItem, 0, len(columns))
	for _, item := range columns {
		blocked := reason
		row.Items = append(row.Items, DailyItem{
			FeedItemLabel: item.Label,
			FeedItemKey:   item.Key,
			Blocked:       &blocked,
		})
	}
	return row
}

// ---------------------------------------------------------------------------
// Experiment workflow: hand-authored absolute quantities
// ---------------------------------------------------------------------------

// ExperimentPlanner serves sheds whose quantities are hand-entered as ABSOLUTE kg for the whole
// shed, rather than derived from the ration grid.
//
// THE ABSOLUTE QUANTITY IS ALREADY A SHED TOTAL. head_count is informational and is never
// multiplied into it -- doing so would overfeed the shed by a factor of its entire population,
// which is the single most important distinction between feed_experiment_config and
// feed_ration_rates. This planner therefore ignores the projected head count for quantity purposes
// entirely and only carries it through for display, flagged as informational.
type ExperimentPlanner struct{}

func (ExperimentPlanner) Workflow() string { return WorkflowExperiment }

// Applies matches a shed that has hand-authored experiment rows. Their presence IS the selection
// rule -- there is no separate "is experiment" flag to fall out of sync with the data.
func (ExperimentPlanner) Applies(shed ShedInput, cfg ConfigSnapshot) bool {
	return len(cfg.ExperimentByLocation[ExperimentLocationKey(shed.ShedID, shed.PartitionLabel)]) > 0
}

// SessionFeedItems returns nil: an experiment shed's items are NOT the park's session slots.
//
// The operator hand-entered exactly what this experiment is fed, so those cells are already the
// complete and authoritative list -- the same reason a catalog item absent from the experiment is
// not a gap here (see PlanDaily). Filtering them through the normal-workflow recipe would delete
// every experiment feed the park's ordinary sessions happen not to declare, which is most of them:
// the experiment workbook's own template names RGS Concentrate and Vijay Concentrate, neither of
// which appears in a normal session.
//
// scoped=false is the "do not filter by session" signal, deliberately not expressible as an empty
// item list, which means the opposite (nothing declared -> block).
func (ExperimentPlanner) SessionFeedItems(ConfigSnapshot, SessionTemplate) ([]FeedItem, bool) {
	return nil, false
}

// grainStage and grainBreed are the two descriptive facets describeGrains can read off a grain.
//
// A stage is reported through the AUTHORED tag vocabulary when it resolves, for the same reason the
// normal path does it (raw live text carries cosmetic variants like 'ICU- kid'). Unlike the normal
// path an unresolvable stage does NOT block here: an experiment quantity is hand-authored and does
// not depend on the tag at all, so falling back to the raw text is strictly more information than
// blanking the column, and there is no ration course to guess wrong.
func grainStage(grain ShedGrain, cfg ConfigSnapshot) string {
	if tag, ok := cfg.ShedTagsByKey[NormalizeConfigKey(grain.ManagementStage)]; ok {
		return tag.Label
	}
	return grain.ManagementStage
}

// grainBreed reports the raw live breed label. There is no authored breed vocabulary to canonicalize
// against -- feed_ration_groups maps a breed to a GROUP, which is a different (and here unused)
// fact -- so the live label is the most specific truth available.
func grainBreed(grain ShedGrain, _ ConfigSnapshot) string { return grain.Breed }

// describeGrains collapses a shed's grains to one honest label for a descriptive column.
//
// Ordering is HEAD COUNT DESCENDING, then label ascending as a deterministic tiebreak. The dominant
// breed or stage reads first, which is what an operator scanning the sheet wants, and the tiebreak
// means the same shed renders identically on every request -- a feed sheet whose columns reorder
// between two loads of the same day is not usable as a printed instruction.
//
// Empty values are skipped rather than joined in, so a shed with one unlabelled grain reports the
// labelled one rather than a dangling " + ".
func describeGrains(grains []ShedGrain, cfg ConfigSnapshot, facet func(ShedGrain, ConfigSnapshot) string) string {
	type entry struct {
		label     string
		headCount int64
	}
	byLabel := map[string]*entry{}
	for _, grain := range grains {
		label := facet(grain, cfg)
		if label == "" {
			continue
		}
		e, ok := byLabel[label]
		if !ok {
			e = &entry{label: label}
			byLabel[label] = e
		}
		e.headCount += grain.HeadCount
	}

	entries := make([]*entry, 0, len(byLabel))
	for _, e := range byLabel {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].headCount != entries[j].headCount {
			return entries[i].headCount > entries[j].headCount
		}
		return entries[i].label < entries[j].label
	})

	labels := make([]string, 0, len(entries))
	for _, e := range entries {
		labels = append(labels, e.label)
	}
	return strings.Join(labels, MultiValueSeparator)
}

// PlanDaily returns ONE row for the whole shed, carrying the authored absolute quantities.
//
// One row, not one per ration grain, because the authored quantity is not per grain: it is a
// single hand-entered figure for the shed. Splitting it across grains would require an allocation
// rule nobody authored.
//
// The authored cells are the COMPLETE list of what this shed is fed. A catalog item with no
// experiment row is not a gap and is not blocked: the operator hand-entered exactly the items this
// experiment uses, so absence here means "not part of this experiment", unlike absence from the
// ration grid which means "nobody said what to feed these animals".
func (ExperimentPlanner) PlanDaily(shed ShedInput, cfg ConfigSnapshot) []DailyRow {
	cells := cfg.ExperimentByLocation[ExperimentLocationKey(shed.ShedID, shed.PartitionLabel)]
	if len(cells) == 0 {
		return nil
	}

	var headCount int64
	overduePending := false
	for _, grain := range shed.Grains {
		headCount += grain.HeadCount
		overduePending = overduePending || grain.OverduePending
	}

	category := ""
	if len(cells) > 0 {
		category = cells[0].Category
	}

	row := DailyRow{
		// THE DESCRIPTIVE COLUMNS COME FROM THE LIVE ANIMALS, exactly as they do on a normal row.
		//
		// An experiment shed has no ration GRAIN -- its quantity is hand-authored and consults
		// neither the tag vocabulary nor the breed map -- but it is still a shed full of animals,
		// and "which animals am I feeding" is the question the operator walks in holding. These two
		// columns used to be the experiment category and an empty string respectively, which meant
		// a real shed of 63 Anantapur Sheep tagged F2-Male printed a blank breed and an arm name
		// where its tag belongs. The arm now travels in its own field below.
		ShedTag: describeGrains(shed.Grains, cfg, grainStage),
		Breed:   describeGrains(shed.Grains, cfg, grainBreed),
		// EMPTY ON PURPOSE, and it is the one column that genuinely has no experiment value: an
		// absolute kg is not derived through a ration group, so naming one would invent a lookup
		// that never happened. See DirectionRow.RationGroup.
		RationGroup:   "",
		ExperimentArm: category,
		HeadCount:     headCount,
		// The flag that stops anything downstream from multiplying by head count.
		HeadCountInformational: true,
		OverduePending:         overduePending,
		Workflow:               WorkflowExperiment,
		Items:                  make([]DailyItem, 0, len(cells)),
	}

	ordered := append([]ExperimentCell(nil), cells...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FeedItemKey < ordered[j].FeedItemKey })

	for _, cell := range ordered {
		item := DailyItem{FeedItemLabel: cell.FeedItemLabel, FeedItemKey: cell.FeedItemKey}
		kg, ok := ParseDecimal(cell.AbsoluteKg)
		if !ok {
			item.Blocked = &BlockedReason{
				Code: BlockReasonNoRationRate,
				Detail: fmt.Sprintf(
					"stored experiment quantity %q for shed %s / item %q is not a valid decimal",
					cell.AbsoluteKg, shed.ShedLabel, cell.FeedItemLabel),
			}
			row.Items = append(row.Items, item)
			continue
		}
		// Authored in kg; the shared pipeline works in grams. NO head-count multiplication.
		item.DailyGrams = kg.Mul(kg, new(big.Rat).SetInt64(1000))
		row.Items = append(row.Items, item)
	}
	return []DailyRow{row}
}
