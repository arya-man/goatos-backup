package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// OperatorDrivePlanner assigns already-eligible vaccination work to operators.
// It does not decide whether an animal is medically due; the vaccination rule
// and obligation layers remain the source of truth for that.
type OperatorDrivePlanner struct {
	MinimumMeaningfulAssignmentAnimals int
}

type DriveOperator struct {
	ID            string
	Name          string
	Cap           int
	ConfiguredCap int
	Available     bool
}

type DriveWorkBlock struct {
	ID           string
	Park         string
	RawShed      string
	PhysicalShed string
	Partition    string
	Animals      int
	// GoatIDs is the EXACT set of animals behind Animals, when the caller knows them. The planner
	// then DECIDES which named animals go to which operator/date arm instead of leaving a count
	// split ("200 on Jul 24, 124 on Jul 25") to be synthesized at persist time. Optional: a
	// count-only block (empty GoatIDs) plans exactly as before and produces unnamed arms.
	GoatIDs        []string
	Species        string
	Bundle         string
	DueDate        time.Time
	LatestSafeDate time.Time
}

type DriveDateAvailability struct {
	Date      time.Time
	Operators []DriveOperator
}

type DrivePlanRequest struct {
	StartDate             time.Time
	Availability          []DriveDateAvailability
	ConfiguredOperatorCap int
	WorkBlocks            []DriveWorkBlock
}

type DrivePlanAssignment struct {
	Date         string
	OperatorID   string
	OperatorName string
	Park         string
	PhysicalShed string
	Partitions   []string
	Animals      int
	// GoatIDs names the animals this arm covers -- the planner's decision, not an artifact of
	// insertion order downstream. Empty exactly when every contributing block was count-only.
	// len(GoatIDs) == Animals whenever the arm's blocks carried identities.
	GoatIDs  []string
	BlockIDs []string
	Warnings []string
}

type DrivePlanDay struct {
	Date        string
	Capacity    int
	Assigned    int
	Assignments []DrivePlanAssignment
}

type DrivePlan struct {
	Days       []DrivePlanDay
	Unassigned []DriveWorkBlock
	Warnings   []string
}

type operatorLoad struct {
	op        DriveOperator
	remaining int
	assigned  int
}

func (p OperatorDrivePlanner) Plan(req DrivePlanRequest) (DrivePlan, error) {
	if req.StartDate.IsZero() {
		return DrivePlan{}, fmt.Errorf("start date is required")
	}
	remaining := normalizeDriveWorkBlocks(req.WorkBlocks)
	sortDriveWorkBlocks(remaining)

	availabilityByDate := make(map[string][]DriveOperator, len(req.Availability))
	dates := make([]string, 0, len(req.Availability))
	for _, day := range req.Availability {
		key := biztime.BusinessDate(day.Date)
		dates = append(dates, key)
		ops := make([]DriveOperator, 0, len(day.Operators))
		for _, op := range day.Operators {
			if op.Available && op.Cap > 0 {
				ops = append(ops, op)
			}
		}
		sort.SliceStable(ops, func(i, j int) bool {
			if ops[i].Name == ops[j].Name {
				return ops[i].ID < ops[j].ID
			}
			return ops[i].Name < ops[j].Name
		})
		availabilityByDate[key] = ops
	}
	sort.Strings(dates)

	plan := DrivePlan{}
	startKey := biztime.BusinessDate(req.StartDate)
	lastPlannedDate := ""
	for _, date := range dates {
		if date < startKey || len(remaining) == 0 {
			continue
		}
		lastPlannedDate = date
		operators := availabilityByDate[date]
		day := DrivePlanDay{Date: date}
		for _, op := range operators {
			day.Capacity += op.Cap
		}
		if len(operators) == 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s has no available vaccination operators", date))
			plan.Days = append(plan.Days, day)
			continue
		}

		assignments, unscheduled := p.planOneDay(date, operators, remaining, configuredOperatorCap(req))
		day.Assignments = assignments
		for _, assignment := range assignments {
			day.Assigned += assignment.Animals
		}
		plan.Days = append(plan.Days, day)
		remaining = unscheduled
	}
	plan.Unassigned = remaining
	for _, block := range remaining {
		if !block.LatestSafeDate.IsZero() && lastPlannedDate >= biztime.BusinessDate(block.LatestSafeDate) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"capacity_breach: %s/%s %s has %d animals beyond latest safe date %s",
				block.Park,
				block.PhysicalShed,
				block.Partition,
				block.Animals,
				biztime.BusinessDate(block.LatestSafeDate),
			))
		}
	}
	return plan, nil
}

func (p OperatorDrivePlanner) planOneDay(date string, operators []DriveOperator, blocks []DriveWorkBlock, configuredOperatorCap int) ([]DrivePlanAssignment, []DriveWorkBlock) {
	loads := make([]operatorLoad, 0, len(operators))
	for _, op := range operators {
		loads = append(loads, operatorLoad{op: op, remaining: op.Cap})
	}

	assignments := make([]DrivePlanAssignment, 0, len(blocks))
	unscheduled := make([]DriveWorkBlock, 0)
	for _, group := range groupWorkBlocksByPhysicalShed(blocks) {
		total := totalBlockAnimals(group.blocks)
		if total <= 0 {
			continue
		}
		choice := bestOperatorForBlock(loads, total)
		if choice < 0 {
			for _, block := range group.blocks {
				if block.Animals <= 0 {
					continue
				}
				choice = bestOperatorForBlock(loads, block.Animals)
				if choice < 0 {
					if configuredOperatorCap > 0 && block.Animals > configuredOperatorCap {
						splitAssignments, residual := splitOversizedBlockAcrossOperators(date, loads, block)
						for _, assignment := range splitAssignments {
							assignments = mergeAssignment(assignments, assignment)
						}
						if residual.Animals > 0 {
							unscheduled = append(unscheduled, residual)
						}
					} else {
						unscheduled = append(unscheduled, block)
					}
					continue
				}
				loads[choice].remaining -= block.Animals
				loads[choice].assigned += block.Animals
				assignments = mergeAssignment(assignments, assignmentForBlock(date, loads[choice].op, block, nil))
			}
			continue
		}
		loads[choice].remaining -= total
		loads[choice].assigned += total
		for _, block := range group.blocks {
			assignments = mergeAssignment(assignments, assignmentForBlock(date, loads[choice].op, block, nil))
		}
	}

	return assignments, unscheduled
}

func configuredOperatorCap(req DrivePlanRequest) int {
	if req.ConfiguredOperatorCap > 0 {
		return req.ConfiguredOperatorCap
	}
	maxCap := 0
	for _, day := range req.Availability {
		for _, op := range day.Operators {
			if !op.Available {
				continue
			}
			if op.ConfiguredCap > maxCap {
				maxCap = op.ConfiguredCap
			}
		}
	}
	if maxCap > 0 {
		return maxCap
	}
	return maxOperatorCap(req.Availability)
}

func maxOperatorCap(availability []DriveDateAvailability) int {
	maxCap := 0
	for _, day := range availability {
		for _, op := range day.Operators {
			if !op.Available {
				continue
			}
			if op.Cap > maxCap {
				maxCap = op.Cap
			}
		}
	}
	return maxCap
}

func splitOversizedBlockAcrossOperators(date string, loads []operatorLoad, block DriveWorkBlock) ([]DrivePlanAssignment, DriveWorkBlock) {
	assignments := make([]DrivePlanAssignment, 0)
	remaining := block.Animals
	// Named animals are handed out in the block's canonical order, so chunk k always covers the same
	// animals for the same inputs; the chunk SIZES are unchanged from the count-only behaviour.
	cursor := 0
	for remaining > 0 {
		choice := bestOperatorWithAnyCapacity(loads)
		if choice < 0 {
			break
		}
		chunk := loads[choice].remaining
		if chunk > remaining {
			chunk = remaining
		}
		chunkBlock := block
		chunkBlock.Animals = chunk
		chunkBlock.GoatIDs = sliceGoatIDs(block.GoatIDs, cursor, chunk)
		cursor += chunk
		assignments = append(assignments, assignmentForBlock(date, loads[choice].op, chunkBlock, []string{"forced_partition_split"}))
		loads[choice].remaining -= chunk
		loads[choice].assigned += chunk
		remaining -= chunk
	}
	residual := block
	residual.Animals = remaining
	residual.GoatIDs = sliceGoatIDs(block.GoatIDs, cursor, remaining)
	return assignments, residual
}

func bestOperatorWithAnyCapacity(loads []operatorLoad) int {
	best := -1
	for i, load := range loads {
		if load.remaining <= 0 {
			continue
		}
		if best < 0 || load.assigned < loads[best].assigned {
			best = i
			continue
		}
		if load.assigned == loads[best].assigned && load.remaining > loads[best].remaining {
			best = i
		}
	}
	return best
}

type physicalShedWorkGroup struct {
	key    string
	blocks []DriveWorkBlock
}

func groupWorkBlocksByPhysicalShed(blocks []DriveWorkBlock) []physicalShedWorkGroup {
	byKey := make(map[string][]DriveWorkBlock)
	keys := make([]string, 0)
	for _, block := range blocks {
		key := block.Park + "\x00" + block.PhysicalShed
		if _, ok := byKey[key]; !ok {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], block)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := byKey[keys[i]]
		right := byKey[keys[j]]
		if left[0].PhysicalShed != right[0].PhysicalShed {
			return left[0].PhysicalShed < right[0].PhysicalShed
		}
		return keys[i] < keys[j]
	})
	groups := make([]physicalShedWorkGroup, 0, len(keys))
	for _, key := range keys {
		group := byKey[key]
		sortDriveWorkBlocks(group)
		groups = append(groups, physicalShedWorkGroup{key: key, blocks: group})
	}
	return groups
}

// canonicalGoatIDs is the planner's stable animal ordering: de-duplicated, blank-free and sorted.
// Every split hands animals out in this order, which is what makes the goat-to-arm mapping
// reproducible across runs and across processes -- unlike Go map iteration or a caller's row order.
func canonicalGoatIDs(goatIDs []string) []string {
	if len(goatIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(goatIDs))
	out := make([]string, 0, len(goatIDs))
	for _, goatID := range goatIDs {
		goatID = strings.TrimSpace(goatID)
		if goatID == "" {
			continue
		}
		if _, dup := seen[goatID]; dup {
			continue
		}
		seen[goatID] = struct{}{}
		out = append(out, goatID)
	}
	sort.Strings(out)
	return out
}

// sliceGoatIDs takes the n names starting at offset, clamped. A count-only block (no names) yields
// nil, so the caller's count behaviour is unchanged.
func sliceGoatIDs(goatIDs []string, offset, n int) []string {
	if len(goatIDs) == 0 || n <= 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(goatIDs) {
		return nil
	}
	end := offset + n
	if end > len(goatIDs) {
		end = len(goatIDs)
	}
	return append([]string(nil), goatIDs[offset:end]...)
}

func totalBlockAnimals(blocks []DriveWorkBlock) int {
	total := 0
	for _, block := range blocks {
		total += block.Animals
	}
	return total
}

func assignmentForBlock(date string, operator DriveOperator, block DriveWorkBlock, warnings []string) DrivePlanAssignment {
	return DrivePlanAssignment{
		Date:         date,
		OperatorID:   operator.ID,
		OperatorName: operator.Name,
		Park:         block.Park,
		PhysicalShed: block.PhysicalShed,
		Partitions:   []string{block.Partition},
		Animals:      block.Animals,
		GoatIDs:      append([]string(nil), block.GoatIDs...),
		BlockIDs:     []string{block.ID},
		Warnings:     warnings,
	}
}

func bestOperatorForBlock(loads []operatorLoad, animals int) int {
	best := -1
	for i, load := range loads {
		if load.remaining < animals {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		if load.assigned < loads[best].assigned {
			best = i
			continue
		}
		if load.assigned == loads[best].assigned && load.remaining < loads[best].remaining {
			best = i
			continue
		}
		if load.assigned == loads[best].assigned &&
			load.remaining == loads[best].remaining &&
			load.op.Name < loads[best].op.Name {
			best = i
		}
	}
	return best
}

func normalizeDriveWorkBlocks(blocks []DriveWorkBlock) []DriveWorkBlock {
	out := make([]DriveWorkBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.PhysicalShed == "" {
			block.PhysicalShed, block.Partition = domain.NormalizeDriveShed(block.RawShed)
		}
		if block.Partition == "" {
			block.Partition = "whole"
		}
		// Names and count must be the same fact, and the name order must be canonical so the
		// goat-to-arm mapping is reproducible run to run (a Go map or a caller's scan order is not).
		if len(block.GoatIDs) > 0 {
			block.GoatIDs = canonicalGoatIDs(block.GoatIDs)
			block.Animals = len(block.GoatIDs)
		}
		out = append(out, block)
	}
	return out
}

func sortDriveWorkBlocks(blocks []DriveWorkBlock) {
	sort.SliceStable(blocks, func(i, j int) bool {
		if !sameBusinessDate(blocks[i].LatestSafeDate, blocks[j].LatestSafeDate) {
			if blocks[i].LatestSafeDate.IsZero() {
				return false
			}
			if blocks[j].LatestSafeDate.IsZero() {
				return true
			}
			return biztime.BusinessDate(blocks[i].LatestSafeDate) < biztime.BusinessDate(blocks[j].LatestSafeDate)
		}
		if !sameBusinessDate(blocks[i].DueDate, blocks[j].DueDate) {
			if blocks[i].DueDate.IsZero() {
				return false
			}
			if blocks[j].DueDate.IsZero() {
				return true
			}
			return biztime.BusinessDate(blocks[i].DueDate) < biztime.BusinessDate(blocks[j].DueDate)
		}
		if blocks[i].PhysicalShed != blocks[j].PhysicalShed {
			return blocks[i].PhysicalShed < blocks[j].PhysicalShed
		}
		if blocks[i].Partition != blocks[j].Partition {
			return partitionLess(blocks[i].Partition, blocks[j].Partition)
		}
		return blocks[i].ID < blocks[j].ID
	})
}

func sameBusinessDate(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() && b.IsZero()
	}
	return biztime.BusinessDate(a) == biztime.BusinessDate(b)
}

func mergeAssignment(assignments []DrivePlanAssignment, next DrivePlanAssignment) []DrivePlanAssignment {
	for i := range assignments {
		existing := &assignments[i]
		if existing.Date == next.Date &&
			existing.OperatorID == next.OperatorID &&
			existing.Park == next.Park &&
			existing.PhysicalShed == next.PhysicalShed {
			existing.Animals += next.Animals
			existing.GoatIDs = canonicalGoatIDs(append(existing.GoatIDs, next.GoatIDs...))
			existing.Partitions = appendUniqueStrings(existing.Partitions, next.Partitions...)
			existing.BlockIDs = append(existing.BlockIDs, next.BlockIDs...)
			existing.Warnings = appendUniqueStrings(existing.Warnings, next.Warnings...)
			return assignments
		}
	}
	return append(assignments, next)
}

func appendUniqueStrings(values []string, next ...string) []string {
	seen := make(map[string]bool, len(values)+len(next))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range next {
		if value == "" || seen[value] {
			continue
		}
		values = append(values, value)
		seen[value] = true
	}
	sort.SliceStable(values, func(i, j int) bool { return partitionLess(values[i], values[j]) })
	return values
}

func partitionLess(a, b string) bool {
	na, oka := trailingInt(a)
	nb, okb := trailingInt(b)
	if oka && okb && na != nb {
		return na < nb
	}
	return a < b
}

func trailingInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	idx := len(value)
	for idx > 0 && value[idx-1] >= '0' && value[idx-1] <= '9' {
		idx--
	}
	if idx == len(value) {
		return 0, false
	}
	n := 0
	for _, ch := range value[idx:] {
		n = n*10 + int(ch-'0')
	}
	return n, true
}
