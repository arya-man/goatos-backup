package domain

import (
	"fmt"
	"math/big"
	"sort"
)

// ---------------------------------------------------------------------------
// The shared generation pipeline
// ---------------------------------------------------------------------------
//
// Everything below is workflow-agnostic. A ShedPlanner produces daily rows; this file splits them
// across the park's sessions, rounds each session to a packable quantity, and encodes the
// blocked-vs-zero distinction into the output type. Both the direction preview and the packing
// worklist go through it, so the two surfaces cannot disagree about what a shed is fed.

// GenerateInput is one generation run: a page of sheds plus the config snapshot they resolve
// against.
type GenerateInput struct {
	Config ConfigSnapshot
	// Sheds is the page of sheds, each carrying its COMPLETE set of projected grains. A partially
	// populated shed would produce a session total that looks whole but is not, which is why the
	// reader pages by shed rather than by grain.
	Sheds []ShedInput
	// SessionNo optionally narrows the output to a single session. Zero means every session. It
	// filters the OUTPUT only -- the split fractions still come from the park's full session set,
	// so asking for the morning session does not silently rescale it to the whole day.
	SessionNo int32
	Rounding  RoundingPolicy
	Planners  PlannerSet
}

// GenerateDirection produces the per-session direction rows for a page of sheds.
//
// It is a pure function: no I/O, no clock, no database. Every business rule this module owns is
// therefore testable without Docker.
func GenerateDirection(in GenerateInput) []DirectionRow {
	out := []DirectionRow{}
	for _, shed := range in.Sheds {
		out = append(out, generateShed(in, shed)...)
	}
	return out
}

func generateShed(in GenerateInput, shed ShedInput) []DirectionRow {
	planner := in.Planners.PlannerFor(shed, in.Config)
	dailyRows := planner.PlanDaily(shed, in.Config)
	if len(dailyRows) == 0 {
		return nil
	}

	// A park with no authored session split cannot be divided into the batches that are actually
	// packed and delivered. That is a configuration gap, so it BLOCKS -- it does not silently
	// collapse to a single implicit "whole day" session, which would hand the packer a number for
	// a session that does not exist.
	if len(in.Config.Sessions) == 0 {
		return blockedSessionRows(in, shed, dailyRows)
	}

	out := make([]DirectionRow, 0, len(dailyRows)*len(in.Config.Sessions))
	for _, session := range in.Config.Sessions {
		if in.SessionNo != 0 && session.SessionNo != in.SessionNo {
			continue
		}
		split, ok := ParseDecimal(session.SplitFraction)
		if !ok {
			continue
		}
		// WHICH items this session consists of is the planner's answer, not the pipeline's -- the
		// grid workflow reads the park's authored slots, the experiment workflow uses the shed's own
		// hand-entered cells. Resolved once per session rather than per row.
		selection, scoped := planner.SessionFeedItems(in.Config, session)
		for _, daily := range dailyRows {
			out = append(out, materializeRow(in, shed, daily, session, split, selection, scoped))
		}
	}
	return out
}

// selectSessionItems narrows one daily row's computed items to the session's declared slots, in
// slot (packing) order.
//
// The daily row carries a quantity for every item declared ANYWHERE in the park (see
// ConfigSnapshot.PlannedFeedItems); this picks out the ones this particular session asks for. When
// the two sets are identical -- which they are for both live parks -- it is a straight reorder.
//
// A declared slot with no computed item is emitted BLOCKED rather than skipped. It is not reachable
// through the normal planner, whose planned set is by construction a superset of every session's
// slots, but silently dropping a declared slot would delete a feed from the packing sheet, so the
// unreachable case still fails loudly rather than quietly.
func selectSessionItems(daily DailyRow, selection []FeedItem, session SessionTemplate) []DailyItem {
	byKey := make(map[string]DailyItem, len(daily.Items))
	for _, item := range daily.Items {
		key := item.FeedItemKey
		if key == "" {
			key = NormalizeConfigKey(item.FeedItemLabel)
		}
		byKey[key] = item
	}

	out := make([]DailyItem, 0, len(selection))
	for _, want := range selection {
		key := want.Key
		if key == "" {
			key = NormalizeConfigKey(want.Label)
		}
		item, ok := byKey[key]
		if !ok {
			out = append(out, DailyItem{
				FeedItemLabel: want.Label,
				FeedItemKey:   key,
				Blocked: &BlockedReason{
					Code: BlockReasonNoRationRate,
					Detail: fmt.Sprintf(
						"feed item %q is declared in session %d but no quantity could be derived for it",
						want.Label, session.SessionNo),
				},
			})
			continue
		}
		out = append(out, item)
	}
	return out
}

// materializeRow applies the session split and the rounding policy to one daily row.
//
// ROUNDING HAPPENS HERE, PER SESSION, on the number that is actually packed -- see the policy note
// in rounding.go. The day's shed total is defined as the sum of these rounded sessions, so the
// sessions add back to the printed total exactly.
func materializeRow(
	in GenerateInput,
	shed ShedInput,
	daily DailyRow,
	session SessionTemplate,
	split *big.Rat,
	selection []FeedItem,
	scoped bool,
) DirectionRow {
	items := daily.Items
	if scoped {
		// A session that declares NO feed items is an incomplete template, not an instruction to
		// feed everything. It blocks, for the same reason a park with no session split blocks: there
		// is no authored answer to what this session consists of, and inventing one (the catalog,
		// say) is how defect 1 shipped 619 kg of a feed nobody packs.
		if len(selection) == 0 {
			return blockedSessionItemsRow(in, shed, daily, session)
		}
		items = selectSessionItems(daily, selection, session)
	}

	row := DirectionRow{
		ParkID:                 in.Config.ParkID,
		ParkLabel:              in.Config.ParkLabel,
		ShedID:                 shed.ShedID,
		ShedLabel:              shed.ShedLabel,
		ShedTag:                daily.ShedTag,
		Breed:                  daily.Breed,
		RationGroup:            daily.RationGroup,
		ExperimentArm:          daily.ExperimentArm,
		SessionNo:              session.SessionNo,
		SessionLabel:           session.Label,
		HeadCount:              daily.HeadCount,
		HeadCountInformational: daily.HeadCountInformational,
		Workflow:               daily.Workflow,
		OverduePending:         daily.OverduePending,
		Items:                  make([]ItemQuantity, 0, len(items)),
	}

	var totalGrams int64
	for _, item := range items {
		quantity := ItemQuantity{FeedItem: item.FeedItemLabel}

		// The blocked branch produces NO number. QuantityKg stays nil, so the value is not
		// representable as zero anywhere downstream -- not in the total below, not in the summary,
		// and not in the JSON a client parses.
		if item.Blocked != nil || item.DailyGrams == nil {
			quantity.Status = QuantityBlocked
			reason := BlockedReason{Code: BlockReasonNoRationRate, Detail: "quantity could not be derived"}
			if item.Blocked != nil {
				reason = *item.Blocked
			}
			quantity.BlockedReason = &reason
			row.Blocked = true
			row.Items = append(row.Items, quantity)
			continue
		}

		sessionGrams := new(big.Rat).Mul(item.DailyGrams, split)
		rounded, kg := RoundSessionGramsToKg(sessionGrams, item.FeedItemKey, in.Rounding)

		quantity.Status = QuantityResolved
		kgValue := kg
		quantity.QuantityKg = &kgValue
		quantity.GramsPerHead = item.GramsPerHead
		quantity.ShedFactor = item.ShedFactor
		totalGrams += rounded
		row.Items = append(row.Items, quantity)
	}

	// The total sums RESOLVED items only. Blocked cells contribute nothing because they have no
	// number to contribute; row.Blocked is what tells the reader the total is partial.
	row.SessionTotalKg = GramsToKgString(totalGrams)
	return row
}

// blockedSessionItemsRow renders one session of a park whose session template declares no feed
// items.
//
// It reuses BlockReasonNoSessionTemplate rather than introducing a code of its own: from an
// operator's point of view both states are the same job -- go and finish the park's session
// template -- and they are actioned on the same config screen. The Detail names which of the two it
// is, which is what an operator needs to locate the gap.
//
// The columns come from blockedColumnItems, so the row still renders with cells (every one blocked,
// none carrying a number) rather than as a short row that reads as "nothing to feed here".
func blockedSessionItemsRow(in GenerateInput, shed ShedInput, daily DailyRow, session SessionTemplate) DirectionRow {
	reason := BlockedReason{
		Code: BlockReasonNoSessionTemplate,
		Detail: fmt.Sprintf(
			"park %s session %d (%s) declares no feed items; there is no authored list of what this session consists of",
			in.Config.ParkLabel, session.SessionNo, session.Label),
	}
	columns := in.Config.blockedColumnItems()
	row := DirectionRow{
		ParkID:                 in.Config.ParkID,
		ParkLabel:              in.Config.ParkLabel,
		ShedID:                 shed.ShedID,
		ShedLabel:              shed.ShedLabel,
		ShedTag:                daily.ShedTag,
		Breed:                  daily.Breed,
		RationGroup:            daily.RationGroup,
		ExperimentArm:          daily.ExperimentArm,
		SessionNo:              session.SessionNo,
		SessionLabel:           session.Label,
		HeadCount:              daily.HeadCount,
		HeadCountInformational: daily.HeadCountInformational,
		Workflow:               daily.Workflow,
		OverduePending:         daily.OverduePending,
		Blocked:                true,
		SessionTotalKg:         GramsToKgString(0),
		Items:                  make([]ItemQuantity, 0, len(columns)),
	}
	for _, item := range columns {
		blocked := reason
		row.Items = append(row.Items, ItemQuantity{
			FeedItem:      item.Label,
			Status:        QuantityBlocked,
			BlockedReason: &blocked,
		})
	}
	return row
}

// blockedSessionRows renders a park with no authored session split. One row per daily row, with a
// zero session number, every item blocked, and the reason stated -- rather than an empty result,
// which would be indistinguishable from an empty shed.
func blockedSessionRows(in GenerateInput, shed ShedInput, dailyRows []DailyRow) []DirectionRow {
	reason := BlockedReason{
		Code: BlockReasonNoSessionTemplate,
		Detail: fmt.Sprintf(
			"park %s has no active feeding-session template; a daily quantity cannot be divided into packable sessions",
			in.Config.ParkLabel),
	}
	out := make([]DirectionRow, 0, len(dailyRows))
	for _, daily := range dailyRows {
		// A park with no session template usually also declares no slots, so the daily row can be
		// item-less. Fall back to the catalog columns rather than emitting a short row that an
		// operator would read as "nothing to feed".
		columns := daily.Items
		if len(columns) == 0 {
			for _, item := range in.Config.blockedColumnItems() {
				columns = append(columns, DailyItem{FeedItemLabel: item.Label, FeedItemKey: item.Key})
			}
		}
		row := DirectionRow{
			ParkID:                 in.Config.ParkID,
			ParkLabel:              in.Config.ParkLabel,
			ShedID:                 shed.ShedID,
			ShedLabel:              shed.ShedLabel,
			ShedTag:                daily.ShedTag,
			Breed:                  daily.Breed,
			RationGroup:            daily.RationGroup,
			ExperimentArm:          daily.ExperimentArm,
			HeadCount:              daily.HeadCount,
			HeadCountInformational: daily.HeadCountInformational,
			Workflow:               daily.Workflow,
			OverduePending:         daily.OverduePending,
			Blocked:                true,
			SessionTotalKg:         GramsToKgString(0),
			Items:                  make([]ItemQuantity, 0, len(columns)),
		}
		for _, item := range columns {
			blocked := reason
			row.Items = append(row.Items, ItemQuantity{
				FeedItem:      item.FeedItemLabel,
				Status:        QuantityBlocked,
				BlockedReason: &blocked,
			})
		}
		out = append(out, row)
	}
	return out
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

// SummarizeScope rolls up the WHOLE FILTERED SET of generated rows.
//
// The caller is responsible for passing every row matching the request's filters, not the page --
// see the contract on PreviewSummary for why the totals must be whole-scope and why they cannot be
// computed in SQL. The service does that by generating the full scope once and then slicing the
// page out of it, so the page rows and these totals come from the SAME GenerateDirection call and
// cannot disagree.
//
// projection-review: membership=every DirectionRow the caller generated for the filtered scope (tenant + park + target_date + optional shed/session), which the service guarantees by generating over the full shed scope from ports.ListShedScope before paging; group_key=NormalizeConfigKey(feed item label) for the per-item totals, the same normalization the generator used to resolve the cell, so a total cannot land in a different bucket than the cell it came from; join_cardinality=no joins -- this is a pure in-memory fold over already-materialized rows, and each cell contributes to exactly one item bucket exactly once; pagination=INVARIANT to limit/offset by construction, because the rows folded here are the whole filtered scope and the page is sliced AFTER this fold; scope=tenant + park + target_date + optional shed_id/session_no, identical to the predicates that selected the rows
//
// Grams, not kg, are accumulated, so the fold stays exact integer arithmetic over values that were
// already rounded per cell. Summing the printed kg strings is what makes the total equal what the
// packer packs -- see PreviewSummary on why re-deriving it from raw grams would under-report.
func SummarizeScope(rows []DirectionRow, items []FeedItem) PreviewSummary {
	summary := PreviewSummary{
		Scope:    SummaryScopeFiltered,
		RowCount: int32(len(rows)),
	}

	sheds := map[string]struct{}{}
	blockedSheds := map[string]struct{}{}
	// Grams, not kg, so the accumulation stays exact integer arithmetic: every contributing value
	// is already a rounded whole number of grams.
	totals := map[string]int64{}
	blockedCells := map[string]int32{}
	labels := map[string]string{}

	for _, row := range rows {
		sheds[row.ShedID] = struct{}{}
		for _, item := range row.Items {
			key := NormalizeConfigKey(item.FeedItem)
			labels[key] = item.FeedItem
			if item.Status == QuantityBlocked || item.QuantityKg == nil {
				summary.BlockedCount++
				blockedCells[key]++
				blockedSheds[row.ShedID] = struct{}{}
				continue
			}
			if grams, ok := kgStringToGrams(*item.QuantityKg); ok {
				totals[key] += grams
			}
		}
	}

	summary.ShedCount = int32(len(sheds))
	summary.BlockedShedCount = int32(len(blockedSheds))

	// Emit in authored catalog order so a client's columns are stable across pages, then append any
	// item seen in the rows but absent from the catalog (an experiment item, for instance) rather
	// than dropping its total.
	summary.TotalKgByFeedItem = feedItemTotals(labels, totals, blockedCells, items)
	return summary
}

// kgStringToGrams parses a fixed-scale kg string back to whole grams.
//
// Exact string arithmetic rather than a float parse: these values are re-summed into a total an
// operator reads, and a float round-trip through "0.100" is precisely how a column total ends up
// reading 12.299999999.
func kgStringToGrams(kg string) (int64, bool) {
	rat, ok := ParseDecimal(kg)
	if !ok {
		return 0, false
	}
	rat.Mul(rat, new(big.Rat).SetInt64(1000))
	if !rat.IsInt() {
		return 0, false
	}
	return rat.Num().Int64(), true
}

// ---------------------------------------------------------------------------
// Packing rollup
// ---------------------------------------------------------------------------

// BuildPackingRows collapses generated direction rows into the per-shed, per-session lines a
// packer works from.
//
// A packer fills ONE bag per feed item per shed per session, not one per ration grain, so the
// grains are summed here. The summation is over the ALREADY-ROUNDED session quantities, which is
// what makes the worklist add up to the direction sheet exactly rather than to a separately
// rounded figure that differs by a few hundred grams.
//
// A blocked cell propagates: if any grain in the shed has an unauthored ration, the shed's line is
// blocked. It is NOT packed from the resolved remainder, because a partially resolved line looks
// like a complete instruction and would send the shed short.
func BuildPackingRows(rows []DirectionRow, items []FeedItem) []PackingRow {
	type lineKey struct {
		shedID    string
		sessionNo int32
	}
	type line struct {
		order   int
		row     PackingRow
		grams   map[string]int64
		blocked map[string]*BlockedReason
		labels  map[string]string
		// Sheds counted once per grain would double-count head count across feed items; the head
		// count is accumulated per ration grain instead, tracked by this set.
		countedGrains map[string]bool
	}

	lines := map[lineKey]*line{}
	order := 0
	for _, row := range rows {
		key := lineKey{shedID: row.ShedID, sessionNo: row.SessionNo}
		l, ok := lines[key]
		if !ok {
			l = &line{
				order: order,
				row: PackingRow{
					ParkID:       row.ParkID,
					ParkLabel:    row.ParkLabel,
					ShedID:       row.ShedID,
					ShedLabel:    row.ShedLabel,
					SessionNo:    row.SessionNo,
					SessionLabel: row.SessionLabel,
					Workflow:     row.Workflow,
					// Safe to take from the first contributing row: the planner is selected per
					// SHED, so every row of a line shares one workflow and therefore one arm
					// (empty for normal).
					ExperimentArm: row.ExperimentArm,
				},
				grams:         map[string]int64{},
				blocked:       map[string]*BlockedReason{},
				labels:        map[string]string{},
				countedGrains: map[string]bool{},
			}
			lines[key] = l
			order++
		}

		grainKey := row.ShedTag + "\x1f" + row.Breed + "\x1f" + row.RationGroup
		if !l.countedGrains[grainKey] {
			l.countedGrains[grainKey] = true
			// An experiment row's head count is informational and is still reported, but the flag
			// travels with it so nothing downstream scales by it.
			l.row.HeadCount += row.HeadCount
		}

		for _, item := range row.Items {
			itemKey := NormalizeConfigKey(item.FeedItem)
			l.labels[itemKey] = item.FeedItem
			if item.Status == QuantityBlocked || item.QuantityKg == nil {
				if _, exists := l.blocked[itemKey]; !exists {
					// P3-BLOCK: a blocked item with a nil BlockedReason is a PROGRAMMER ERROR, not a
					// legitimate "no reason given" state -- every blocking call site in this package
					// (normalItem, blockedRow, blockedSessionItemsRow, ...) sets one. Silently
					// substituting an empty BlockedReason{} here used to produce a packing row that
					// LOOKS blocked (Status == "blocked") but carries an empty code/detail an
					// operator or API consumer cannot act on, and it would hide the real upstream
					// bug that failed to set a reason. Fail loudly instead of shipping that.
					if item.BlockedReason == nil {
						panic(fmt.Sprintf(
							"feeddirection: BuildPackingRows: blocked item %q (shed %s, session %d) has a nil BlockedReason",
							item.FeedItem, row.ShedID, row.SessionNo))
					}
					reason := *item.BlockedReason
					l.blocked[itemKey] = &reason
				}
				continue
			}
			if grams, ok := kgStringToGrams(*item.QuantityKg); ok {
				l.grams[itemKey] += grams
			}
		}
	}

	ordered := make([]*line, 0, len(lines))
	for _, l := range lines {
		ordered = append(ordered, l)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })

	out := make([]PackingRow, 0, len(ordered))
	for _, l := range ordered {
		row := l.row
		var total int64
		reasons := []BlockedReason{}
		seenReason := map[string]bool{}

		emit := func(itemKey, label string) {
			if reason, blocked := l.blocked[itemKey]; blocked {
				r := *reason
				row.Items = append(row.Items, ItemQuantity{
					FeedItem:      label,
					Status:        QuantityBlocked,
					BlockedReason: &r,
				})
				if !seenReason[r.Code+r.Detail] {
					seenReason[r.Code+r.Detail] = true
					reasons = append(reasons, r)
				}
				return
			}
			grams := l.grams[itemKey]
			total += grams
			kg := GramsToKgString(grams)
			row.Items = append(row.Items, ItemQuantity{
				FeedItem:   label,
				Status:     QuantityResolved,
				QuantityKg: &kg,
			})
		}

		seen := map[string]bool{}
		for _, item := range items {
			key := NormalizeConfigKey(item.Label)
			if _, present := l.labels[key]; !present {
				continue
			}
			seen[key] = true
			emit(key, item.Label)
		}
		extra := make([]string, 0)
		for key := range l.labels {
			if !seen[key] {
				extra = append(extra, key)
			}
		}
		sort.Strings(extra)
		for _, key := range extra {
			emit(key, l.labels[key])
		}

		row.TotalKg = GramsToKgString(total)
		switch {
		case len(reasons) > 0:
			row.Status = PackingStatusBlocked
			row.BlockedReasons = reasons
		case row.HeadCount == 0 && total == 0:
			// Nothing to feed is NOT a configuration gap, and conflating the two would send an
			// operator hunting for a missing rate that does not exist.
			row.Status = PackingStatusEmpty
		default:
			row.Status = PackingStatusReady
		}
		out = append(out, row)
	}
	return out
}

// SummarizePacking rolls up the WHOLE FILTERED worklist.
//
// It folds the ALREADY-SUMMED packing lines rather than re-folding the direction rows, so the store
// draw it reports is by construction the sum of the lines a packer works through. Re-deriving it
// from the grains would be a second computation that could disagree with the printed worklist.
//
// projection-review: membership=every PackingRow the caller built for the filtered scope, which the service guarantees by building the worklist over the full shed scope before paging; group_key=NormalizeConfigKey(feed item label), the same normalization BuildPackingRows used to merge grains into each line's bag, so a line's cell and its contribution to the total share one bucket; join_cardinality=no joins -- a pure in-memory fold, and because BuildPackingRows already collapsed grains to one cell per (shed, session, item) there is no fan-out for a shed's multiple grains to double-count; pagination=INVARIANT to limit/offset, the fold runs over the whole filtered scope and the page is sliced afterwards; scope=tenant + park + target_date, identical to the predicates that selected the lines
func SummarizePacking(lines []PackingRow, items []FeedItem) PackingSummary {
	summary := PackingSummary{
		Scope:     SummaryScopeFiltered,
		LineCount: int32(len(lines)),
	}

	sheds := map[string]struct{}{}
	blockedSheds := map[string]struct{}{}
	totals := map[string]int64{}
	blockedCells := map[string]int32{}
	labels := map[string]string{}

	for _, line := range lines {
		sheds[line.ShedID] = struct{}{}
		if line.Status == PackingStatusBlocked {
			summary.BlockedLineCount++
		}
		for _, item := range line.Items {
			key := NormalizeConfigKey(item.FeedItem)
			labels[key] = item.FeedItem
			// Blocked stays unrepresentable as a number here, exactly as it is on the row: it is
			// counted as a gap, never added as zero.
			if item.Status == QuantityBlocked || item.QuantityKg == nil {
				summary.BlockedCount++
				blockedCells[key]++
				blockedSheds[line.ShedID] = struct{}{}
				continue
			}
			if grams, ok := kgStringToGrams(*item.QuantityKg); ok {
				totals[key] += grams
			}
		}
	}

	summary.ShedCount = int32(len(sheds))
	summary.BlockedShedCount = int32(len(blockedSheds))
	summary.TotalKgByFeedItem = feedItemTotals(labels, totals, blockedCells, items)
	return summary
}

// feedItemTotals emits per-item totals in authored catalog display order, then appends any item
// seen in the data but absent from the catalog (an experiment shed's hand-entered feed, say) rather
// than dropping its total.
//
// Shared by both summaries so the two surfaces cannot order or omit their columns differently.
func feedItemTotals(
	labels map[string]string,
	totals map[string]int64,
	blockedCells map[string]int32,
	items []FeedItem,
) []FeedItemTotal {
	seen := map[string]bool{}
	out := make([]FeedItemTotal, 0, len(labels))
	for _, item := range items {
		key := NormalizeConfigKey(item.Label)
		if _, ok := labels[key]; !ok {
			continue
		}
		seen[key] = true
		out = append(out, FeedItemTotal{
			FeedItem:     item.Label,
			QuantityKg:   GramsToKgString(totals[key]),
			BlockedCells: blockedCells[key],
		})
	}
	extra := make([]string, 0)
	for key := range labels {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		out = append(out, FeedItemTotal{
			FeedItem:     labels[key],
			QuantityKg:   GramsToKgString(totals[key]),
			BlockedCells: blockedCells[key],
		})
	}
	return out
}
