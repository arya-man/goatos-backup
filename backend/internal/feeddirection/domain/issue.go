package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ---------------------------------------------------------------------------
// Issue lifecycle: the FROZEN, issued feed sheet
// ---------------------------------------------------------------------------
//
// Feed Direction is no longer a live calculator. A sheet for feed day D is ISSUED once (on D-1 at
// the park/workflow direction_time), frozen immutably, optionally AMENDED once at correction_time
// for the sheds an emergency shifting moved, and finally LOCKED at transport_time. This file holds
// the pure, database-free model of that: the states, the stored-cell shape, the fingerprint that
// makes a re-issue a no-op, the flatten/reconstruct pair that round-trips a generated sheet through
// storage byte-for-byte, and the diff an amendment is built from.

// Issue lifecycle states. Mutated in place on one row; the *_at instants accumulate.
const (
	// IssueStateIssued -- generated once and frozen. The default state on creation.
	IssueStateIssued = "issued"
	// IssueStateAmended -- a correction batch changed some sheds after the sheet was issued.
	IssueStateAmended = "amended"
	// IssueStateLocked -- the transport cutoff passed; no further change is allowed and a later
	// change rolls to the next feed day.
	IssueStateLocked = "locked"
)

// Serving-only lifecycle states, reported by the read path when there is nothing frozen yet. They
// are deliberately distinct from an ISSUED sheet: the whole point of this feature is to answer
// "not yet issued" honestly instead of live-computing a speculative number.
const (
	// LifecycleStateDraft -- a live what-if compute (draft=true), explicitly NOT an issued sheet.
	LifecycleStateDraft = "draft"
	// LifecycleStatePreview -- the requested feed day has NO issued sheet, so the serve path GENERATES
	// the full scope on demand and returns it, labelled as a not-yet-issued preview (maintainer
	// decision 2026-07-20: "whenever I ask for feed data by date, generate the feed for that date").
	// It is distinct from Draft (Draft is the explicit config-authoring what-if escape hatch) and from
	// an ISSUED sheet (which serves FROZEN stored rows). The per-workflow detail still carries
	// pending/not_issued and the expected issue instant, so an operator can see when the sheet WILL be
	// formally frozen.
	LifecycleStatePreview = "preview"
	// LifecycleStatePending -- the feed day is in the future beyond its issue instant; the sheet
	// will be issued at a known time. Retained as a PER-WORKFLOW state inside a preview lifecycle so
	// the operator sees the expected issue time; the serve path no longer returns it as an aggregate
	// empty wall.
	LifecycleStatePending = "pending"
	// LifecycleStateNotIssued -- the issue instant has passed and no sheet was ever issued. Retained
	// as a PER-WORKFLOW state inside a preview lifecycle (the aggregate is preview, with rows).
	LifecycleStateNotIssued = "not_issued"
	// LifecycleStateBeyondHorizon -- the requested feed day has NO issued sheet AND falls OUTSIDE the
	// [today, tomorrow] projection window, so the serve path REFUSES to generate rather than
	// fabricating a sheet (maintainer decision 2026-07-20). The projected shed count = live herd +
	// approved-but-unexecuted shiftings is only meaningful for today (being fed) and tomorrow (being
	// packed now); beyond tomorrow the counts depend on shiftings not yet approved, and a past day's
	// herd is not what it is now. Either way, generating would silently freeze today's herd onto the
	// wrong day. The aggregate carries this state with NO rows and an operator sentence naming the
	// horizon; it is NEVER stored on an issue (an already-issued sheet for any date still serves its
	// frozen rows -- the guard applies ONLY to on-the-fly generation).
	LifecycleStateBeyondHorizon = "beyond_horizon"
)

// SourceContract / version stamped on every issue for provenance, mirroring the counts snapshots.
const (
	SourceContract        = "feed-direction-issue-v1"
	SourceContractVersion = "v1"
)

// IssueHeader is one feed_direction_issues row in this module's terms.
type IssueHeader struct {
	IssueID                    string
	TenantID                   string
	ParkID                     string
	FeedDay                    string // Asia/Kolkata business date, YYYY-MM-DD
	Workflow                   string
	State                      string
	IssuedAt                   time.Time
	AmendedAt                  *time.Time
	LockedAt                   *time.Time
	GenerationInputFingerprint string
	AmendmentCount             int32
}

// StoredCell is one feed_direction_issue_rows row: a single (grain, session, feed_item) cell of the
// frozen sheet, denormalized flat. It is the storage shape of one ItemQuantity plus the grain and
// shed context needed to reconstruct its DirectionRow.
//
// BLOCKED-VS-ZERO IS THE POINTER CONTRACT, PRESERVED. QuantityKg is nil if and only if the cell is
// blocked; a resolved authored zero is a non-nil "0.000". BlockedReasonCode is set if and only if
// QuantityKg is nil. Nothing here can turn a blocked cell into a numeric zero.
type StoredCell struct {
	ParkID                 string
	ParkLabel              string
	ShedID                 string
	ShedLabel              string
	ShedTag                string
	Breed                  string
	RationGroup            string
	ExperimentArm          string
	SessionNo              int32
	SessionLabel           string
	HeadCount              int64
	HeadCountInformational bool
	Workflow               string
	FeedItemLabel          string
	FeedItemKey            string
	QuantityKg             *string
	GramsPerHead           *string
	ShedFactor             *string
	BlockedReasonCode      *string
	BlockedReasonDetail    *string
	SessionTotalKg         string
	OverduePending         bool
	RowSeq                 int32
	ItemSeq                int32
	// Amended marks a cell an amendment changed. Only meaningful on the read/diff side.
	Amended bool
}

// CellKey is the natural identity of a stored cell WITHIN an issue: the grain plus the feed item.
// shed_tag and breed are normalized because that is the grain group key the generator uses, so two
// grains of a multi-grain shed never collapse and a cosmetic spelling variant never splits one.
type CellKey struct {
	ShedID      string
	SessionNo   int32
	ShedTagKey  string
	BreedKey    string
	FeedItemKey string
}

// Key returns the cell's natural identity.
func (c StoredCell) Key() CellKey {
	return CellKey{
		ShedID:      c.ShedID,
		SessionNo:   c.SessionNo,
		ShedTagKey:  NormalizeConfigKey(c.ShedTag),
		BreedKey:    NormalizeConfigKey(c.Breed),
		FeedItemKey: c.FeedItemKey,
	}
}

// FingerprintRows hashes a generated sheet into a stable fingerprint of its herd + config inputs.
//
// It is computed over the OUTPUT rows rather than the raw inputs on purpose: the output is a total
// function of the inputs plus the generation logic, so an identical output proves identical inputs
// AND identical rules, which is exactly what "did anything change" must mean for a re-issue no-op or
// an amend diff. It captures every field that alters what a packer packs: head count, the
// blocked-vs-zero state and quantity of every cell, and the session total. Rows arrive in
// deterministic generation order, so the same sheet always hashes the same.
func FingerprintRows(rows []DirectionRow) string {
	h := sha256.New()
	for _, r := range rows {
		fmt.Fprintf(h, "R\x1f%s\x1f%d\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%t\x1f%s\x1f%s\x1f%t\n",
			r.ShedID, r.SessionNo, r.ShedTag, r.Breed, r.RationGroup, r.ExperimentArm,
			r.HeadCount, r.HeadCountInformational, r.Workflow, r.SessionTotalKg, r.OverduePending)
		for _, it := range r.Items {
			quantity := "BLOCKED"
			if it.QuantityKg != nil {
				quantity = *it.QuantityKg
			}
			code := ""
			if it.BlockedReason != nil {
				code = it.BlockedReason.Code
			}
			fmt.Fprintf(h, "  I\x1f%s\x1f%s\x1f%s\x1f%s\n", it.FeedItem, it.Status, quantity, code)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// FlattenRows turns generated DirectionRows into storable cells, stamping generation order into
// RowSeq/ItemSeq so the sheet reconstructs in the exact order it was written.
//
// A blocked cell always gets a reason code -- falling back to the generic no-ration-rate code if
// the generator somehow produced a blocked item without one -- so the storage layer's blocked-shape
// CHECK (blocked iff a reason and no quantity) can never be violated.
func FlattenRows(rows []DirectionRow) []StoredCell {
	out := make([]StoredCell, 0)
	for ri, row := range rows {
		for ii, item := range row.Items {
			cell := StoredCell{
				ParkID:                 row.ParkID,
				ParkLabel:              row.ParkLabel,
				ShedID:                 row.ShedID,
				ShedLabel:              row.ShedLabel,
				ShedTag:                row.ShedTag,
				Breed:                  row.Breed,
				RationGroup:            row.RationGroup,
				ExperimentArm:          row.ExperimentArm,
				SessionNo:              row.SessionNo,
				SessionLabel:           row.SessionLabel,
				HeadCount:              row.HeadCount,
				HeadCountInformational: row.HeadCountInformational,
				Workflow:               row.Workflow,
				FeedItemLabel:          item.FeedItem,
				FeedItemKey:            NormalizeConfigKey(item.FeedItem),
				SessionTotalKg:         row.SessionTotalKg,
				OverduePending:         row.OverduePending,
				RowSeq:                 int32(ri),
				ItemSeq:                int32(ii),
			}
			if item.Status == QuantityBlocked || item.QuantityKg == nil {
				code := BlockReasonNoRationRate
				detail := "quantity could not be derived"
				if item.BlockedReason != nil {
					code = item.BlockedReason.Code
					detail = item.BlockedReason.Detail
				}
				cell.BlockedReasonCode = &code
				cell.BlockedReasonDetail = &detail
			} else {
				cell.QuantityKg = copyString(item.QuantityKg)
				cell.GramsPerHead = copyString(item.GramsPerHead)
				cell.ShedFactor = copyString(item.ShedFactor)
			}
			out = append(out, cell)
		}
	}
	return out
}

// ReconstructRows rebuilds the DirectionRows of ONE issue from its stored cells.
//
// It is called per issue and the results concatenated, because RowSeq is scoped to a single issue's
// generation; grouping cells from two issues by RowSeq would merge unrelated rows. Ordering is
// RowSeq then ItemSeq, so the frozen sheet comes back exactly as it was stored.
func ReconstructRows(cells []StoredCell) []DirectionRow {
	type bucket struct {
		row   DirectionRow
		cells []StoredCell
	}
	buckets := map[int32]*bucket{}
	order := make([]int32, 0)
	for _, cell := range cells {
		b, ok := buckets[cell.RowSeq]
		if !ok {
			b = &bucket{row: DirectionRow{
				ParkID:                 cell.ParkID,
				ParkLabel:              cell.ParkLabel,
				ShedID:                 cell.ShedID,
				ShedLabel:              cell.ShedLabel,
				ShedTag:                cell.ShedTag,
				Breed:                  cell.Breed,
				RationGroup:            cell.RationGroup,
				ExperimentArm:          cell.ExperimentArm,
				SessionNo:              cell.SessionNo,
				SessionLabel:           cell.SessionLabel,
				HeadCount:              cell.HeadCount,
				HeadCountInformational: cell.HeadCountInformational,
				Workflow:               cell.Workflow,
				SessionTotalKg:         cell.SessionTotalKg,
				OverduePending:         cell.OverduePending,
			}}
			buckets[cell.RowSeq] = b
			order = append(order, cell.RowSeq)
		}
		b.cells = append(b.cells, cell)
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]DirectionRow, 0, len(order))
	for _, seq := range order {
		b := buckets[seq]
		sort.Slice(b.cells, func(i, j int) bool { return b.cells[i].ItemSeq < b.cells[j].ItemSeq })
		row := b.row
		row.Items = make([]ItemQuantity, 0, len(b.cells))
		for _, cell := range b.cells {
			item := ItemQuantity{FeedItem: cell.FeedItemLabel}
			if cell.QuantityKg == nil {
				item.Status = QuantityBlocked
				reason := BlockedReason{Code: BlockReasonNoRationRate}
				if cell.BlockedReasonCode != nil {
					reason.Code = *cell.BlockedReasonCode
				}
				if cell.BlockedReasonDetail != nil {
					reason.Detail = *cell.BlockedReasonDetail
				}
				item.BlockedReason = &reason
				row.Blocked = true
			} else {
				item.Status = QuantityResolved
				item.QuantityKg = copyString(cell.QuantityKg)
				item.GramsPerHead = copyString(cell.GramsPerHead)
				item.ShedFactor = copyString(cell.ShedFactor)
			}
			row.Items = append(row.Items, item)
		}
		out = append(out, row)
	}
	return out
}

// DistinctFeedItems returns the feed items across a set of rows in first-seen order. It is the
// column order the whole-scope summary renders in when serving stored rows -- the same order the
// rows were generated in, so served columns match what was frozen.
func DistinctFeedItems(rows []DirectionRow) []FeedItem {
	seen := map[string]bool{}
	out := make([]FeedItem, 0)
	for _, row := range rows {
		for _, item := range row.Items {
			key := NormalizeConfigKey(item.FeedItem)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, FeedItem{Label: item.FeedItem, Key: key})
		}
	}
	return out
}

// CellDiff is the result of comparing a freshly generated sheet against the stored one.
type CellDiff struct {
	// Changed carries every new-or-different cell, ready to upsert-and-mark-amended.
	Changed []StoredCell
	// RemovedKeys are cells that were in the stored sheet but are gone from the new one (a shed
	// emptied, a grain merged away). The amend deletes them.
	RemovedKeys []CellKey
	// AffectedShedIDs is the distinct set of sheds any change touched -- the "affected sheds only"
	// an amendment records.
	AffectedShedIDs []string
}

// HasChanges reports whether the amend actually moved anything.
func (d CellDiff) HasChanges() bool { return len(d.Changed) > 0 || len(d.RemovedKeys) > 0 }

// DiffCells compares the stored cells against a freshly generated set, cell by cell on the natural
// key, and reports exactly what an amendment must write.
//
// A cell is "changed" when any packable value differs -- the quantity, the blocked state/reason, the
// head count, the session total, or a descriptive column. Everything else is left untouched, which
// is what makes an amendment mark ONLY the affected sheds.
func DiffCells(stored, generated []StoredCell) CellDiff {
	storedByKey := make(map[CellKey]StoredCell, len(stored))
	for _, cell := range stored {
		storedByKey[cell.Key()] = cell
	}
	generatedByKey := make(map[CellKey]struct{}, len(generated))

	diff := CellDiff{}
	affected := map[string]struct{}{}
	for _, cell := range generated {
		key := cell.Key()
		generatedByKey[key] = struct{}{}
		prev, ok := storedByKey[key]
		if ok && cellPayload(prev) == cellPayload(cell) {
			continue
		}
		diff.Changed = append(diff.Changed, cell)
		affected[cell.ShedID] = struct{}{}
	}
	for key, cell := range storedByKey {
		if _, ok := generatedByKey[key]; !ok {
			diff.RemovedKeys = append(diff.RemovedKeys, key)
			affected[cell.ShedID] = struct{}{}
		}
	}

	diff.AffectedShedIDs = make([]string, 0, len(affected))
	for shed := range affected {
		diff.AffectedShedIDs = append(diff.AffectedShedIDs, shed)
	}
	sort.Strings(diff.AffectedShedIDs)
	return diff
}

// cellPayload is the value-equality projection of a stored cell: everything that determines what a
// packer packs or reads, excluding the amendment bookkeeping (Amended) and the generation ordinals,
// which are not business facts about the cell.
func cellPayload(c StoredCell) string {
	return strings.Join([]string{
		c.ParkLabel, c.ShedLabel, c.ShedTag, c.Breed, c.RationGroup, c.ExperimentArm,
		c.SessionLabel, strconv.FormatInt(c.HeadCount, 10), strconv.FormatBool(c.HeadCountInformational),
		c.Workflow, c.FeedItemLabel, derefString(c.QuantityKg, "BLOCKED"),
		derefString(c.GramsPerHead, ""), derefString(c.ShedFactor, ""),
		derefString(c.BlockedReasonCode, ""), derefString(c.BlockedReasonDetail, ""),
		c.SessionTotalKg, strconv.FormatBool(c.OverduePending),
	}, "\x1f")
}

// ---------------------------------------------------------------------------
// Read-path lifecycle metadata
// ---------------------------------------------------------------------------

// WorkflowLifecycle is one workflow's issue state, reported alongside a served or pending sheet.
type WorkflowLifecycle struct {
	Workflow       string  `json:"workflow"`
	State          string  `json:"state"`
	IssuedAt       *string `json:"issued_at,omitempty"`
	AmendedAt      *string `json:"amended_at,omitempty"`
	LockedAt       *string `json:"locked_at,omitempty"`
	AmendmentCount int32   `json:"amendment_count"`
	// ExpectedIssueAt is set for the pending / not_issued states: the instant the sheet is (or was)
	// due to be issued, so the UI can say "issued <D-1> at <direction_time> IST" instead of showing
	// a speculative number.
	ExpectedIssueAt *string `json:"expected_issue_at,omitempty"`
}

// Lifecycle is the aggregate lifecycle of a served park-day, rolled up across its workflows.
//
// The aggregate State is the LEAST-ADVANCED live state among the present issues (issued < amended <
// locked), so an operator is never told a park-day is "locked" while one of its two workflows is
// still merely issued, nor "amended" while another is still plainly issued. When nothing is issued
// the state is pending (every expected workflow is still in the future) or not_issued (its issue
// instant has passed), and draft for the live what-if path.
type Lifecycle struct {
	State          string  `json:"state"`
	IssuedAt       *string `json:"issued_at,omitempty"`
	AmendedAt      *string `json:"amended_at,omitempty"`
	LockedAt       *string `json:"locked_at,omitempty"`
	AmendmentCount int32   `json:"amendment_count"`
	// Message is the operator sentence, most useful for pending/not_issued.
	Message string `json:"message,omitempty"`
	// Workflows carries the per-workflow detail behind the aggregate.
	Workflows []WorkflowLifecycle `json:"workflows"`
}

// FormatBusinessInstant renders an instant as an RFC3339 string in the Asia/Kolkata business
// calendar. Business meaning is always India-local; the UI reads the wall clock the operator keeps.
func FormatBusinessInstant(t time.Time) string {
	return t.In(biztime.DefaultLocation()).Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Dispatch clock (feed_schedule_config)
// ---------------------------------------------------------------------------

// WorkflowClock is one (park, workflow) dispatch clock from feed_schedule_config. The times are
// LOCAL Asia/Kolkata wall-clock rules ("07:00 at the park", forever), never instants -- they are
// combined with a business date to get the instant.
type WorkflowClock struct {
	Workflow string
	// DirectionTime / CorrectionTime are "HH:MM:SS" local times; TransportTime is nil when the park
	// has not declared a transport cutoff (which must read as UNKNOWN, never "no deadline").
	DirectionTime  string
	CorrectionTime string
	TransportTime  *string
}

// ExpectedIssueInstant returns the instant a sheet for feedDay is (or was) due to be issued: the
// direction_time on D-1, in Asia/Kolkata. Feed for day D is produced on D-1, so a sheet's issue
// instant is one business day before its feed day.
func (c WorkflowClock) ExpectedIssueInstant(feedDay string) (time.Time, error) {
	return combineLocalTime(feedDay, -1, c.DirectionTime)
}

// combineLocalTime parses a YYYY-MM-DD business date, shifts it by dayOffset days, and attaches an
// "HH:MM:SS" local wall-clock time, all in Asia/Kolkata.
func combineLocalTime(day string, dayOffset int, localTime string) (time.Time, error) {
	loc := biztime.DefaultLocation()
	base, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("feeddirection: parse feed_day %q: %w", day, err)
	}
	base = base.AddDate(0, 0, dayOffset)
	clock, err := time.ParseInLocation("15:04:05", strings.TrimSpace(localTime), loc)
	if err != nil {
		// Postgres time can arrive as HH:MM; accept it too.
		clock, err = time.ParseInLocation("15:04", strings.TrimSpace(localTime), loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("feeddirection: parse direction_time %q: %w", localTime, err)
		}
	}
	return time.Date(base.Year(), base.Month(), base.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, loc), nil
}

func copyString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func derefString(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
