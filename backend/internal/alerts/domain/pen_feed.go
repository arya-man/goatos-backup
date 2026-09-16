package domain

import (
	"fmt"
	"math"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// PenFeedDay is one pen's feed sheet for one feed day, rolled up from the FROZEN
// feed_direction_issue_rows: the pen's head count (the sum over the pen's ration grains
// for one session -- every session feeds the same animals, so the max across sessions is
// the pen's count) and its total packable kg across every session and feed item. It is
// the projection the feed module already froze, read back, never a recount of the herd.
type PenFeedDay struct {
	ParkID         string
	ParkLabel      string
	ShedID         string
	ShedName       string
	PartitionLabel string
	HeadCount      int64
	QuantityKg     float64
	// BlockedCells counts sheet cells with no authored ration (quantity NULL).
	BlockedCells int64
}

func (p PenFeedDay) penKey() string {
	return p.ShedID + "|" + oploc.NormalizePartition(p.PartitionLabel)
}

func (p PenFeedDay) location() oploc.OperationalLocation {
	return oploc.OperationalLocation{ShedName: p.ShedName, PartitionLabel: p.PartitionLabel}
}

// PenMovement is one shifting event that touched the park between the two sheets, with the
// animals it moves. Source is blank for a movement into the park from outside herd scope.
type PenMovement struct {
	SourceShedID              string
	SourcePartitionLabel      string
	DestinationShedID         string
	DestinationPartitionLabel string
	HeadCount                 int64
	// Status is the shifting register's event_status, kept so the detail can say whether
	// the movement that explains a change is still awaiting approval.
	Status string
}

// PenFeedChangeInput is everything the detector needs for ONE park.
type PenFeedChangeInput struct {
	BusinessDate string
	// Today and Yesterday are the two frozen sheets. A nil Yesterday (no sheet was issued)
	// yields no alerts: there is nothing to compare against, and inventing a baseline would
	// raise an alert for every pen on the farm's first day.
	Today     []PenFeedDay
	Yesterday []PenFeedDay
	Movements []PenMovement
	// MinHeadChange is the rule's threshold: head-count moves smaller than this are ignored.
	MinHeadChange int64
}

// DetectPenFeedChanges compares the two sheets pen by pen and raises one alert per pen
// whose change the recorded shiftings do not explain.
//
// The rule (maintainer request 2026-09-16): a change in a pen's quantity against yesterday
// is expected only when animals moved. So a head-count change is checked against the net
// shifting delta for that pen; a match is silent, no movement is critical, a partial match
// is a warning naming how much is accounted for. A kg change with the SAME head count has
// no movement to explain it at all, so it always fires (a ration or config edit). Cells
// newly blocked since yesterday fire too: a pen that was fed yesterday and has no ration
// today is off, whatever moved.
func DetectPenFeedChanges(in PenFeedChangeInput) []Alert {
	if len(in.Yesterday) == 0 && len(in.Today) == 0 {
		return nil
	}
	if in.Yesterday == nil {
		return nil
	}
	minChange := in.MinHeadChange
	if minChange < 1 {
		minChange = 1
	}
	today := map[string]PenFeedDay{}
	for _, p := range in.Today {
		today[p.penKey()] = p
	}
	yesterday := map[string]PenFeedDay{}
	for _, p := range in.Yesterday {
		yesterday[p.penKey()] = p
	}
	// Net recorded movement per pen: +animals arriving, -animals leaving.
	net := map[string]int64{}
	pending := map[string]bool{}
	for _, m := range in.Movements {
		if m.DestinationShedID != "" {
			k := m.DestinationShedID + "|" + oploc.NormalizePartition(m.DestinationPartitionLabel)
			net[k] += m.HeadCount
			if m.Status == "pending" || m.Status == "authorized" {
				pending[k] = true
			}
		}
		if m.SourceShedID != "" {
			k := m.SourceShedID + "|" + oploc.NormalizePartition(m.SourcePartitionLabel)
			net[k] -= m.HeadCount
			if m.Status == "pending" || m.Status == "authorized" {
				pending[k] = true
			}
		}
	}
	keys := map[string]struct{}{}
	for k := range today {
		keys[k] = struct{}{}
	}
	for k := range yesterday {
		keys[k] = struct{}{}
	}
	rule, _ := RuleByKey(RulePenFeedQuantityChange)
	out := []Alert{}
	for k := range keys {
		t, hasT := today[k]
		y, hasY := yesterday[k]
		ref := t
		if !hasT {
			ref = y
		}
		var todayHead, yestHead int64
		var todayKg, yestKg float64
		if hasT {
			todayHead, todayKg = t.HeadCount, t.QuantityKg
		}
		if hasY {
			yestHead, yestKg = y.HeadCount, y.QuantityKg
		}
		dHead := todayHead - yestHead
		dKg := todayKg - yestKg
		base := Alert{
			RuleKey:                    rule.Key,
			RuleLabel:                  rule.Label,
			ParkID:                     ref.ParkID,
			ParkLabel:                  ref.ParkLabel,
			ShedID:                     ref.ShedID,
			ShedName:                   ref.ShedName,
			PartitionLabel:             partitionForWire(ref.PartitionLabel),
			OperationalLocationDisplay: ref.location().Display(),
			BusinessDate:               in.BusinessDate,
			Href:                       "/feed/direction",
		}
		pen := base.OperationalLocationDisplay
		if pen == "" {
			pen = "this pen"
		}
		switch {
		case abs64(dHead) >= minChange:
			explained := net[k]
			if explained == dHead {
				// Every animal that arrived or left is on the shifting register: nothing off.
				break
			}
			a := base
			a.Key = fmt.Sprintf("%s:%s:%s:head", rule.Key, in.BusinessDate, k)
			a.Title = fmt.Sprintf("%s: %d → %d animals on the feed sheet", pen, yestHead, todayHead)
			switch {
			case explained == 0:
				a.Severity = SeverityCritical
				a.Detail = fmt.Sprintf("Head count moved by %s and feed by %s kg, with no shifting recorded for this pen. Check the herd register and raise the movement, or correct the pen.", signed(dHead), signedKg(dKg))
			default:
				a.Severity = SeverityWarning
				qualifier := ""
				if pending[k] {
					qualifier = " (some still awaiting approval)"
				}
				a.Detail = fmt.Sprintf("Head count moved by %s, but the recorded shiftings account for %s%s. Feed moved by %s kg.", signed(dHead), signed(explained), qualifier, signedKg(dKg))
			}
			out = append(out, a)
		case dHead == 0 && hasT && hasY && math.Abs(dKg) >= 0.05:
			a := base
			a.Key = fmt.Sprintf("%s:%s:%s:kg", rule.Key, in.BusinessDate, k)
			a.Severity = SeverityWarning
			a.Title = fmt.Sprintf("%s: feed %.1f → %.1f kg with the same %d animals", pen, yestKg, todayKg, todayHead)
			a.Detail = "Nothing moved in or out, so the change is a ration or feed-config edit. Confirm it was intended."
			out = append(out, a)
		}
		if hasT && t.BlockedCells > 0 && (!hasY || y.BlockedCells == 0) {
			a := base
			a.Key = fmt.Sprintf("%s:%s:%s:blocked", rule.Key, in.BusinessDate, k)
			a.Severity = SeverityCritical
			a.Title = fmt.Sprintf("%s: %d feed line(s) have no ration today", pen, t.BlockedCells)
			a.Detail = "The sheet was fed yesterday and has cells with no authored ration today. Check Feed Config for this pen's tag and breed."
			out = append(out, a)
		}
	}
	SortAlerts(out)
	return out
}

// partitionForWire keeps the matching sentinel ('whole') off the wire: an undivided pen carries
// a blank partition, never the key the storage layer joins on.
func partitionForWire(raw string) string {
	if !oploc.IsPartitioned(raw) {
		return ""
	}
	return strings.TrimSpace(raw)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func signed(v int64) string {
	if v > 0 {
		return fmt.Sprintf("+%d", v)
	}
	return fmt.Sprintf("%d", v)
}

func signedKg(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	if v > 0 {
		return "+" + s
	}
	return strings.TrimSpace(s)
}
