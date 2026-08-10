package domain

import (
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Direction-sheet collapse
// ---------------------------------------------------------------------------
//
// The GENERATOR works at the ration grain -- (shed + partition, shed tag, breed) -- because that is
// the grain a ration rate is actually looked up at, and it is the grain the frozen issue stores.
// The DIRECTION SHEET does not: an operator standing at a pen door feeds the pen, so the sheet has
// exactly one row per operational location per session (maintainer decision 2026-08-10, superseding
// the "BREED IS NOT summed away" rule that NormalPlanner.PlanDaily still correctly applies to the
// grain).
//
// # WHY THIS IS A SERVE-PATH FOLD AND NOT A CHANGE TO THE PLANNER
//
// Packing must not move, and it is built from these same rows. Collapsing earlier -- in the planner,
// or at issue time -- would sum GRAMS and round once, while packing sums ALREADY-ROUNDED session
// quantities. Rounding is up-to-a-step and therefore non-linear: sum(ceil(x)) != ceil(sum(x)), so
// two grains needing 1.2 kg and 1.3 kg of a 1 kg-step item are 4 kg packed today and would become
// 3 kg. That is a real underfeed introduced by a display change. So the generator, the stored issue
// and BuildPackingRows are all left untouched, and this fold runs over the rounded per-grain rows on
// the direction read only -- the identical arithmetic BuildPackingRows already performs, which is
// what keeps the sheet and the bag equal to the gram.
//
// # A GAP DOES NOT BLANK THE CELL
//
// Maintainer decision 2026-08-10: on the direction sheet a merged cell prints what IS configured,
// and the gap is named on the row, so the operator feeds the animals they have a ration for and
// records their video rather than being handed a blank. That is a DIRECTION-only rule; the packing
// line still blocks whole, because a bag packed from a partial number looks complete and would send
// the shed short.
//
// The blocked-vs-zero contract is untouched underneath it: a cell where NOTHING resolved still has
// a nil QuantityKg and is still QuantityBlocked, so an unauthored ration can never be read as 0.

// CollapseDirectionRowsByLocation folds generated direction rows to ONE row per (shed, partition,
// session).
//
// The key is the same lineKey BuildPackingRows uses -- shed id, normalized partition key, session
// number -- deliberately, so a direction row and the bag packed from it always describe the same
// physical pen. Input order is preserved: the first row of each location fixes that location's
// position, which keeps a printed sheet stable across requests.
//
// Rows that are already one-per-location (every experiment sheet) pass through unchanged apart from
// re-deriving their own descriptive columns, which is a no-op on a single contributor.
func CollapseDirectionRowsByLocation(rows []DirectionRow) []DirectionRow {
	// WORKFLOW IS PART OF THE KEY, and it is load-bearing rather than defensive. A shed-session's
	// completion is recorded per workflow (see the app layer's completedKey), so two workflows at one
	// location are two independent completions with two independent verification verdicts. Merging
	// them would stamp the merged row with whichever completion arrived first and report the other as
	// pending -- an operator's finished work reading as outstanding, or the reverse.
	//
	// It is reachable: the normal sheet freezes at 07:00 and the experiment sheet at 14:00 from the
	// same park-day, and the serve path UNIONS both issues, so a location that gains or loses
	// experiment cells between those two clocks appears in both. It also cannot resurrect the
	// per-breed split this fold exists to remove -- the planner is chosen per location, so all of one
	// location's grains in one sheet share a workflow.
	type key struct {
		shedID       string
		partitionKey string
		sessionNo    int32
		workflow     string
	}

	groups := map[key]*locationGroup{}
	order := 0
	for _, row := range rows {
		k := key{
			shedID:       row.ShedID,
			partitionKey: PartitionMatchKey(row.PartitionLabel),
			sessionNo:    row.SessionNo,
			workflow:     row.Workflow,
		}
		g, ok := groups[k]
		if !ok {
			merged := row
			merged.Items = nil
			merged.HeadCount = 0
			merged.Blocked = false
			merged.BlockedReasons = nil
			merged.SessionTotalKg = GramsToKgString(0)
			g = &locationGroup{
				order:        order,
				row:          merged,
				tags:         map[string]*facetEntry{},
				breeds:       map[string]*facetEntry{},
				rationGroups: map[string]*facetEntry{},
				cells:        map[string]*locationCell{},
				seenReason:   map[string]bool{},
			}
			groups[k] = g
			order++
		}
		// Head counts are per ration grain and every grain appears exactly once per session, so this
		// is a straight sum. HeadCountInformational travels from the first contributor: the planner is
		// chosen per location, so every row of one location shares a workflow.
		g.row.HeadCount += row.HeadCount
		g.row.OverduePending = g.row.OverduePending || row.OverduePending

		addFacet(g.tags, row.ShedTag, row.HeadCount)
		addFacet(g.breeds, row.Breed, row.HeadCount)
		addFacet(g.rationGroups, row.RationGroup, row.HeadCount)

		for _, item := range row.Items {
			itemKey := NormalizeConfigKey(item.FeedItem)
			c, ok := g.cells[itemKey]
			if !ok {
				c = &locationCell{order: len(g.cells), label: item.FeedItem}
				g.cells[itemKey] = c
			}

			if item.Status == QuantityBlocked || item.QuantityKg == nil {
				if c.blocked == nil && item.BlockedReason != nil {
					reason := *item.BlockedReason
					c.blocked = &reason
				}
				// The gap is reported on the row whether or not the cell ends up with a number, which
				// is what lets an operator see WHICH group is unconfigured while still feeding the rest.
				if item.BlockedReason != nil {
					g.addReason(*item.BlockedReason)
				}
				g.row.Blocked = true
				continue
			}

			grams, ok := kgStringToGrams(*item.QuantityKg)
			if !ok {
				continue
			}
			if c.resolved {
				if !samePtrString(c.gramsPerHead, item.GramsPerHead) {
					c.rateConflict = true
				}
				if !samePtrString(c.shedFactor, item.ShedFactor) {
					c.factorConflict = true
				}
			} else {
				c.gramsPerHead = item.GramsPerHead
				c.shedFactor = item.ShedFactor
			}
			c.grams += grams
			c.resolved = true
		}
	}

	ordered := make([]*locationGroup, 0, len(groups))
	for _, g := range groups {
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })

	out := make([]DirectionRow, 0, len(ordered))
	for _, g := range ordered {
		row := g.row
		row.ShedTag = joinFacet(g.tags)
		row.Breed = joinFacet(g.breeds)
		row.RationGroup = joinFacet(g.rationGroups)

		cells := make([]*locationCell, 0, len(g.cells))
		for _, c := range g.cells {
			cells = append(cells, c)
		}
		// First-seen order, which is the session's authored slot order -- the same order the
		// contributing rows already printed their columns in.
		sort.Slice(cells, func(i, j int) bool { return cells[i].order < cells[j].order })

		var total int64
		row.Items = make([]ItemQuantity, 0, len(cells))
		for _, c := range cells {
			quantity := ItemQuantity{FeedItem: c.label}
			if !c.resolved {
				// NOTHING resolved for this item anywhere in the pen. It stays blocked and numberless:
				// there is no configured quantity to show, so there is nothing to feed from.
				quantity.Status = QuantityBlocked
				reason := BlockedReason{Code: BlockReasonNoRationRate, Detail: "quantity could not be derived"}
				if c.blocked != nil {
					reason = *c.blocked
				}
				quantity.BlockedReason = &reason
				row.Items = append(row.Items, quantity)
				continue
			}
			quantity.Status = QuantityResolved
			kg := GramsToKgString(c.grams)
			quantity.QuantityKg = &kg
			if !c.rateConflict {
				quantity.GramsPerHead = c.gramsPerHead
			}
			if !c.factorConflict {
				quantity.ShedFactor = c.shedFactor
			}
			total += c.grams
			row.Items = append(row.Items, quantity)
		}
		row.SessionTotalKg = GramsToKgString(total)
		row.BlockedReasons = g.reasons
		out = append(out, row)
	}
	return out
}

// facetEntry accumulates one descriptive column value with the head count standing behind it, so the
// join can order values the way describeGrains does: dominant value first.
type facetEntry struct {
	order     int
	headCount int64
}

// locationCell is one feed item's accumulated quantity across every ration grain in the location.
type locationCell struct {
	order int
	label string
	// grams sums the RESOLVED contributions only.
	grams int64
	// resolved is true once any contributor produced a number. A cell with no resolved contributor
	// stays blocked and numberless -- the blocked-vs-zero contract, unchanged.
	resolved bool
	// gramsPerHead / shedFactor are echoed only while every resolved contributor agrees: two ration
	// groups in one pen have two different rates, and neither may be printed as "the" rate.
	gramsPerHead   *string
	shedFactor     *string
	rateConflict   bool
	factorConflict bool
	blocked        *BlockedReason
}

// locationGroup accumulates every ration grain of one (shed, partition, session) into the single row
// the direction sheet prints for it.
type locationGroup struct {
	order int
	row   DirectionRow
	// tags, breeds and rationGroups are the three joined descriptive columns.
	tags         map[string]*facetEntry
	breeds       map[string]*facetEntry
	rationGroups map[string]*facetEntry
	cells        map[string]*locationCell
	reasons      []BlockedReason
	seenReason   map[string]bool
}

// addReason records a distinct gap for the merged row. Deduplicated on code+detail because the same
// missing ration cell is reported once per contributing grain, and an operator needs the list of
// gaps, not a tally of how many rows noticed each one.
func (g *locationGroup) addReason(reason BlockedReason) {
	key := reason.Code + "\x1f" + reason.Detail
	if g.seenReason[key] {
		return
	}
	g.seenReason[key] = true
	g.reasons = append(g.reasons, reason)
}

// addFacet accumulates one descriptive value's head count. Empty values are skipped so an
// experiment row's deliberately blank ration group cannot produce a dangling separator.
func addFacet(into map[string]*facetEntry, label string, headCount int64) {
	if label == "" {
		return
	}
	e, ok := into[label]
	if !ok {
		e = &facetEntry{order: len(into)}
		into[label] = e
	}
	e.headCount += headCount
}

// joinFacet renders a descriptive column: distinct values, head count descending, label ascending as
// the deterministic tiebreak, joined by MultiValueSeparator. Identical ordering to describeGrains,
// which the experiment planner already uses -- the dominant breed or stage reads first, and the same
// pen renders identically on every request.
func joinFacet(values map[string]*facetEntry) string {
	if len(values) == 0 {
		return ""
	}
	type entry struct {
		label     string
		headCount int64
	}
	entries := make([]entry, 0, len(values))
	for label, e := range values {
		entries = append(entries, entry{label: label, headCount: e.headCount})
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

// samePtrString compares two optional echoed values. Two nils agree; a nil and a value do not.
func samePtrString(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}
