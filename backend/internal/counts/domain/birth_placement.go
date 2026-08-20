package domain

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Newborn placement: a kid is born into a K0 pen, never into whatever pen the operator happens to
// pick (maintainer decision 2026-08-20).
//
// THE DEFECT THIS REPLACES. The birth form offered the FULL park -> shed -> pen cascade -- the same
// catalog the shifting form uses -- and validated nothing beyond "the shed is active and sits under
// this park". A K0 kid could therefore be recorded into a Buck shed, an F2 pen, or an ICU pen, and
// nothing downstream ever noticed: the newborn's management_stage is pinned K0 by the birth handler
// regardless of where it lands, so the animal read as a kid while physically filed with the bucks.
//
// THE RULE IS RESOLVED FROM THE PEN'S AUTHORED TAG, not from its residents. ShiftingDestinationShed
// .ConfiguredStage is already exactly that -- the pen's own shed_partitions.animal_stage_id, or the
// shed's profile for a shed with no pens (migration 000161). Deriving "is this a kid pen" from who
// currently stands in it would answer differently tomorrow as animals move, and an EMPTY K0 pen --
// the normal state of a pen kept ready for kids -- would not resolve at all.
//
// This whole file is PURE. It is the ONE implementation of the rule: the destinations read
// (app_destinations_handler) composes the operator's form contract from it, and the birth write
// (RecordBirthEvent) validates the submitted pen against it. Two implementations would drift, and
// the operator would be shown one answer while the write enforced another.

// NewbornPenStage is the cohort tag that marks a pen as the place newborns go.
//
// Re-exported from protocol/domain, which is the neutral home for "what does this management_stage
// mean" so Counts and Tasks can both ask without importing each other. It is deliberately the same
// string as the birth handler's newbornManagementStage: the pen a kid is placed in is tagged for
// the cohort that kid carries.
const NewbornPenStage = protocoldomain.NewbornPenStage

// Birth placement modes. The MODE is backend-owned and the phone renders it; the phone must not
// re-derive "how many kid pens does this park have" from the pen list, because the answer also
// governs whether the write will accept a freely chosen pen.
const (
	// BirthPlacementModeAutomatic: the park has exactly ONE kid pen. The operator does not choose;
	// the form shows the pen read-only and the write accepts only that pen.
	BirthPlacementModeAutomatic = "automatic"
	// BirthPlacementModeChoose: the park has MORE THAN ONE kid pen. The operator picks, and the
	// picker offers only those pens (maintainer decision 2026-08-20: the operator picks; no
	// automatic ranking, because an ORDER BY over pens that legitimately tie fabricates an answer
	// that flips as animals move).
	BirthPlacementModeChoose = "choose"
	// BirthPlacementModeRecordLater: the park has NO kid pen configured. A birth is never lost over
	// missing configuration, so the operator picks freely from the full cascade and the kid's care
	// workflow carries a Record shed step that confirms the pen and tags it for kids.
	BirthPlacementModeRecordLater = "record_later"
)

// Farm-worded notices, rendered VERBATIM by the phone (golden frontend rule). No internal
// vocabulary: an operator reads about kid pens, not about modes, catalogs or validation.
const (
	birthPlacementNoticeAutomaticPrefix = "Kids born in this park go to "
	birthPlacementNoticeChoose          = "Choose which kid pen this kid goes into."
	birthPlacementNoticeRecordLater     = "This park has no kid pen set yet. Record where the kid is now, and confirm its pen in the kid's care steps."
)

// BirthPlacementPen is one selectable newborn destination.
//
// It carries the full operational location -- shed id, shed name, partition label and the
// backend-composed display -- because location crosses about five handoffs and dropping any half
// renders a bare shed name end to end. Display is oploc-composed upstream and is never re-derived
// on the client.
type BirthPlacementPen struct {
	ShedID         string
	ShedName       string
	PartitionLabel *string
	Display        string
}

// BirthPlacementResolution is the whole newborn-placement contract for ONE park.
type BirthPlacementResolution struct {
	Mode   string
	Notice string
	// Pens is the kid pens of this park, empty in record_later mode. In automatic mode it holds
	// exactly one entry.
	Pens []BirthPlacementPen
}

// IsNewbornPen reports whether a pen's AUTHORED tag marks it as a kid pen.
//
// Case-insensitive and trimmed because the tag is authored data read back from
// animal_stage_lookup, and a tenant that seeded "k0" means the same pen as one that seeded "K0".
func IsNewbornPen(configuredStage string) bool {
	return protocoldomain.IsNewbornPen(configuredStage)
}

// ResolveBirthPlacement decides where a newborn in this park may be placed, from the park's own
// catalog rows.
//
// sheds is every operational location of ONE park (one row per real pen, or exactly one row with a
// nil PartitionLabel for a genuinely non-partitioned shed). Passing a single park's rows is what
// keeps a kid pen in the OTHER park from ever being offered: parks are resolved independently and
// nothing here reads across them.
func ResolveBirthPlacement(sheds []ShiftingDestinationShed) BirthPlacementResolution {
	pens := make([]BirthPlacementPen, 0, len(sheds))
	for _, shed := range sheds {
		if !IsNewbornPen(shed.ConfiguredStage) {
			continue
		}
		pens = append(pens, BirthPlacementPen{
			ShedID:         shed.ShedID,
			ShedName:       shed.Name,
			PartitionLabel: shed.PartitionLabel,
			Display:        shed.Display,
		})
	}
	switch len(pens) {
	case 0:
		return BirthPlacementResolution{
			Mode:   BirthPlacementModeRecordLater,
			Notice: birthPlacementNoticeRecordLater,
			Pens:   []BirthPlacementPen{},
		}
	case 1:
		return BirthPlacementResolution{
			Mode:   BirthPlacementModeAutomatic,
			Notice: birthPlacementNoticeAutomaticPrefix + pens[0].Display + ".",
			Pens:   pens,
		}
	default:
		return BirthPlacementResolution{
			Mode:   BirthPlacementModeChoose,
			Notice: birthPlacementNoticeChoose,
			Pens:   pens,
		}
	}
}

// AllowsPen reports whether a submitted (shed, partition) pair is a placement this park permits.
//
// record_later permits ANY pen: the park has no kid pen to insist on, and refusing the birth would
// lose a real event over missing configuration. The other two modes permit only the kid pens, and
// the write REJECTS anything else rather than silently correcting it -- a silent correction would
// file the kid somewhere the operator never saw and never told them.
//
// The partition halves are compared with oploc.SamePartition, never with ==: the stored label
// ('Part 3') and a client's echo of it normalize to the same pen, and a nil/blank label means the
// bare non-partitioned shed on both sides.
func (r BirthPlacementResolution) AllowsPen(shedID string, partitionLabel *string) bool {
	if r.Mode == BirthPlacementModeRecordLater {
		return true
	}
	shedID = strings.TrimSpace(shedID)
	want := ""
	if partitionLabel != nil {
		want = strings.TrimSpace(*partitionLabel)
	}
	for _, pen := range r.Pens {
		if pen.ShedID != shedID {
			continue
		}
		have := ""
		if pen.PartitionLabel != nil {
			have = strings.TrimSpace(*pen.PartitionLabel)
		}
		if have == "" && want == "" {
			return true
		}
		if have != "" && want != "" && oploc.SamePartition(have, want) {
			return true
		}
	}
	return false
}
