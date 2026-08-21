package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Sale allocation: pick the real animals a recorded sale is made of.
//
// THREE STEPS, AND THE MIDDLE ONE IS THE POINT:
//
//	list     -> the picker: animals of a park/shed/pen, each already judged
//	preview  -> the review: what you picked, shed by shed, and what is refused. NO WRITE.
//	confirm  -> record the allocation and exit each animal as sold. ONE transaction.
//
// THE GATE RUNS IN GO, ONCE. domain.ResolveSaleBlocker is called by judge() below and by
// nothing else, so the greyed-out row in the picker, the refusal in the review, and the
// refusal at confirm are the same verdict by construction. Computing it in SQL for the
// list and in Go for the confirm is how a quarantined animal ends up sellable on one path.
//
// THE CONFIRM NEVER TRUSTS THE PREVIEW. It re-reads every animal and re-runs the gate at
// commit time. A preview is a snapshot of a moment, and between that moment and the
// confirm a real animal can be put in quarantine, treated, moved or sold by someone else.
// Trusting a token would sell an animal whose state has since changed -- and the state
// change is precisely the thing the gate exists to catch.

// SaleAllocationService owns the three steps.
type SaleAllocationService struct {
	reader ports.SaleAllocationReader
	writer ports.SaleAllocationWriter
	deals  ports.SaleDealReader
}

func NewSaleAllocationService(reader ports.SaleAllocationReader, writer ports.SaleAllocationWriter, deals ports.SaleDealReader) *SaleAllocationService {
	return &SaleAllocationService{reader: reader, writer: writer, deals: deals}
}

// judge turns a raw row into the judged candidate the client renders. THE ONLY caller of
// domain.ResolveSaleBlocker in this package.
func judge(row ports.SaleCandidateRow, businessToday string) ports.SaleCandidate {
	verdict := domain.ResolveSaleBlocker(row.State, businessToday)
	return ports.SaleCandidate{
		GoatID:         row.GoatID,
		DisplayID:      row.DisplayID,
		TagNumber:      row.TagNumber,
		ParkID:         row.ParkID,
		ParkName:       row.ParkName,
		ShedID:         row.ShedID,
		ShedName:       row.ShedName,
		PartitionLabel: row.PartitionLabel,
		OperationalLocationDisplay: (oploc.OperationalLocation{
			ShedID:         row.ShedID,
			ShedName:       row.ShedName,
			PartitionLabel: row.PartitionLabel,
		}).Display(),
		Breed:           row.Breed,
		Sex:             row.Sex,
		ManagementStage: row.State.ManagementStage,
		RowVersion:      row.State.RowVersion,
		Blocker:         string(verdict.Blocker),
		BlockedReason:   verdict.Reason,
		Sellable:        verdict.Sellable(),
	}
}

// ListSaleCandidatesInput filters the picker.
type ListSaleCandidatesInput struct {
	TenantID        string
	ParkID          string
	ShedID          string
	PartitionLabels []string
	Query           string
	Limit           int
	Cursor          string
}

// ListSaleCandidates is the picker read: the animals of a park/shed/pen with each one's
// verdict already attached, keyset-paged.
//
// Blocked animals are RETURNED, not filtered out. A person who can see an animal standing
// in the pen but cannot find it in the list concludes the system is broken; "In
// quarantine" against the row answers the question instead, and the client renders it
// unselectable.
func (s *SaleAllocationService) ListSaleCandidates(ctx context.Context, input ListSaleCandidatesInput) ([]ports.SaleCandidate, *string, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return nil, nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	parkID := strings.TrimSpace(input.ParkID)
	if !uuidPattern.MatchString(parkID) {
		return nil, nil, BadRequest("invalid_park_id", "park_id must be a valid UUID")
	}
	shedID := strings.TrimSpace(input.ShedID)
	if shedID != "" && !uuidPattern.MatchString(shedID) {
		return nil, nil, BadRequest("invalid_shed_id", "shed_id must be a valid UUID")
	}
	limit := input.Limit
	if limit <= 0 || limit > ports.SaleCandidatePageSize {
		limit = ports.SaleCandidatePageSize
	}

	rows, cursor, err := s.reader.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
		TenantID:        tenantID,
		ParkID:          parkID,
		ShedID:          shedID,
		PartitionLabels: trimAll(input.PartitionLabels),
		Query:           strings.TrimSpace(input.Query),
		IncludeBlocked:  true,
		Limit:           limit,
		Cursor:          strings.TrimSpace(input.Cursor),
	})
	if err != nil {
		return nil, nil, mapRepoErr(err)
	}
	today := biztime.BusinessDate(time.Now().UTC())
	out := make([]ports.SaleCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, judge(row, today))
	}
	return out, cursor, nil
}

// PreviewSaleAllocationInput names the deal and the picked animals.
type PreviewSaleAllocationInput struct {
	TenantID    string
	SalesDealID string
	GoatIDs     []string
}

// PreviewSaleAllocation is the review step: what you picked, grouped shed-wise, with the
// refused animals and their reasons. It MUTATES NOTHING.
func (s *SaleAllocationService) PreviewSaleAllocation(ctx context.Context, input PreviewSaleAllocationInput) (*ports.SaleAllocationPreview, error) {
	tenantID, dealID, goatIDs, err := validateAllocationInput(input.TenantID, input.SalesDealID, input.GoatIDs)
	if err != nil {
		return nil, err
	}
	deal, err := s.deals.ReadSaleDeal(ctx, tenantID, dealID)
	if err != nil {
		return nil, mapSaleDealErr(err)
	}
	candidates, err := s.judgeNamedAnimals(ctx, tenantID, dealID, goatIDs)
	if err != nil {
		return nil, err
	}

	preview := &ports.SaleAllocationPreview{
		SalesDealID:         dealID,
		DeclaredAnimalCount: deal.DeclaredAnimalCount,
		AlreadyTagged:       deal.AlreadyTagged,
		BlockedAnimals:      []ports.SaleCandidate{},
	}
	sellable := make([]ports.SaleCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Sellable {
			sellable = append(sellable, c)
			continue
		}
		preview.BlockedAnimals = append(preview.BlockedAnimals, c)
	}
	// Disjoint and exhaustive: every named animal lands in exactly one bucket, so a
	// client can render "12 ready, 2 blocked" without a third overlapping count.
	preview.Sellable = len(sellable)
	preview.Blocked = len(preview.BlockedAnimals)
	preview.ShedGroups = groupByShed(sellable)
	// The screen shows "x of N", and the same arithmetic decides whether Confirm is
	// offered at all -- one rule, not a client-side opinion about a server-side gate.
	preview.Complete = preview.Sellable == deal.Remaining() && preview.Blocked == 0
	return preview, nil
}

// ConfirmSaleAllocationInput is the confirm.
//
// There is deliberately NO override field. See domain.SaleBlocker: an override would put a
// contagious or residue-carrying animal on a buyer's truck on one person's say-so.
type ConfirmSaleAllocationInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	SalesDealID    string
	GoatIDs        []string
	Reason         string
}

// ConfirmSaleAllocation records the allocation and exits each animal as sold, in ONE
// transaction.
//
// FAIL-CLOSED AND ALL-OR-NOTHING. If ANY named animal is refused by the gate, the whole
// confirm is rejected and nothing is written. Applying the sellable ones and silently
// dropping the rest would leave the person believing they sold 14 animals when the farm
// recorded 12, and the two animals they physically loaded would still read as in-herd.
func (s *SaleAllocationService) ConfirmSaleAllocation(ctx context.Context, input ConfirmSaleAllocationInput) (*ports.SaleAllocationResult, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	_, dealID, goatIDs, err := validateAllocationInput(tenantID, input.SalesDealID, input.GoatIDs)
	if err != nil {
		return nil, err
	}

	deal, err := s.deals.ReadSaleDeal(ctx, tenantID, dealID)
	if err != nil {
		return nil, mapSaleDealErr(err)
	}

	// Re-read and re-judge at commit time. The preview is never trusted.
	candidates, err := s.judgeNamedAnimals(ctx, tenantID, dealID, goatIDs)
	if err != nil {
		return nil, err
	}
	blocked := make([]ports.SaleCandidate, 0)
	rows := make([]ports.SaleAllocationRow, 0, len(candidates))
	for _, c := range candidates {
		if !c.Sellable {
			blocked = append(blocked, c)
			continue
		}
		rows = append(rows, ports.SaleAllocationRow{GoatID: c.GoatID, RowVersion: c.RowVersion})
	}
	if len(blocked) > 0 {
		return nil, blockedConflict(blocked)
	}
	if len(rows) == 0 {
		return nil, BadRequest("no_animals", "select at least one animal to tag to this sale")
	}
	// THE COUNT GATE (maintainer rule 2026-08-20). A sale is for a stated number of
	// animals, and the mapping must hit it EXACTLY -- no partial mapping, no overshoot.
	//
	// Enforced HERE, on the server, against a count read from the ledger rather than one
	// supplied by the caller: a client-side cap is a hint, not a gate, and a crafted
	// request would simply claim a different target. Checked AFTER the re-judge so the
	// number compared is the number that would actually be written.
	if err := checkSaleCountGate(deal, len(rows)); err != nil {
		return nil, err
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "Sold to buyer"
	}
	result, err := s.writer.RecordSaleAllocations(ctx, ports.RecordSaleAllocationsCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: strings.Join([]string{tenantID, "sale_allocation", dealID, clientKey}, ":"),
		TraceID:              input.TraceID,
		SalesDealID:          dealID,
		DeclaredAnimalCount:  deal.DeclaredAnimalCount,
		Rows:                 rows,
		Reason:               reason,
		OccurredAt:           time.Now().UTC(),
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return result, nil
}

// judgeNamedAnimals reads the named set in ONE query and runs the gate over it.
//
// An id the read did not return is judged as a MISSING animal rather than dropped: a
// person who picked 14 and is shown 13 with no explanation has no way to find out what
// happened to the fourteenth.
func (s *SaleAllocationService) judgeNamedAnimals(ctx context.Context, tenantID, dealID string, goatIDs []string) ([]ports.SaleCandidate, error) {
	rows, err := s.reader.ReadSaleCandidateRows(ctx, tenantID, dealID, goatIDs)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	today := biztime.BusinessDate(time.Now().UTC())
	out := make([]ports.SaleCandidate, 0, len(goatIDs))
	for _, id := range goatIDs {
		row, ok := rows[id]
		if !ok {
			row = ports.SaleCandidateRow{GoatID: id, State: domain.SaleCandidateState{GoatID: id}}
		}
		out = append(out, judge(row, today))
	}
	return out, nil
}

// groupByShed builds the gather list: the picked animals of one operational shed, in the
// order a person walks the farm. Tag numbers are sorted so two previews of the same
// selection read identically.
func groupByShed(candidates []ports.SaleCandidate) []ports.SaleAllocationShedGroup {
	type key struct{ shedID, partition string }
	index := map[key]*ports.SaleAllocationShedGroup{}
	order := []key{}
	for _, c := range candidates {
		// Keyed on shed ID, never shed NAME: two parks genuinely own a shed called
		// "Castro", and name-keying would merge them into one gather list.
		k := key{shedID: c.ShedID, partition: c.PartitionLabel}
		group, ok := index[k]
		if !ok {
			group = &ports.SaleAllocationShedGroup{
				ParkName:                   c.ParkName,
				ShedID:                     c.ShedID,
				ShedName:                   c.ShedName,
				PartitionLabel:             c.PartitionLabel,
				OperationalLocationDisplay: c.OperationalLocationDisplay,
				TagNumbers:                 []string{},
			}
			index[k] = group
			order = append(order, k)
		}
		group.Animals++
		if tag := strings.TrimSpace(c.TagNumber); tag != "" {
			group.TagNumbers = append(group.TagNumbers, tag)
		}
	}
	out := make([]ports.SaleAllocationShedGroup, 0, len(order))
	for _, k := range order {
		group := index[k]
		sort.Strings(group.TagNumbers)
		out = append(out, *group)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ParkName != out[j].ParkName {
			return out[i].ParkName < out[j].ParkName
		}
		return out[i].OperationalLocationDisplay < out[j].OperationalLocationDisplay
	})
	return out
}

func validateAllocationInput(tenantID, dealID string, goatIDs []string) (string, string, []string, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return "", "", nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	deal := strings.TrimSpace(dealID)
	if !uuidPattern.MatchString(deal) {
		return "", "", nil, BadRequest("invalid_sales_deal_id", "sales_deal_id must be a valid UUID")
	}
	// De-duplicated in order. A client that sends the same animal twice means it once,
	// and letting the duplicate through would double the head count on the review screen.
	seen := map[string]bool{}
	out := make([]string, 0, len(goatIDs))
	for _, raw := range goatIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if !uuidPattern.MatchString(id) {
			return "", "", nil, BadRequest("invalid_goat_id", "every goat_id must be a valid UUID")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return "", "", nil, BadRequest("no_animals", "select at least one animal to tag to this sale")
	}
	if len(out) > ports.MaxSaleAllocationGoatsPerCommand {
		return "", "", nil, BadRequest("too_many_animals",
			"a single confirmation can carry at most "+strconv.Itoa(ports.MaxSaleAllocationGoatsPerCommand)+" animals")
	}
	return tenant, deal, out, nil
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		if v := strings.TrimSpace(raw); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// blockedConflict refuses the whole confirm and names EVERY refused animal with its
// reason.
//
// Naming them all matters: a person who picked 14 animals and is told only "1 blocked"
// has to re-open the picker and hunt. The message carries the identifier they read off
// the animal and the farm sentence, so the next action is obvious from the error alone.
//
// 422 rather than 409: the request is well-formed and the caller is entitled to make it;
// the FARM's state is what refuses it. That distinction matters to a client deciding
// whether to offer a retry -- retrying this unchanged will always fail, and it should say
// so rather than spin.
func blockedConflict(blocked []ports.SaleCandidate) *Error {
	parts := make([]string, 0, len(blocked))
	for _, c := range blocked {
		label := strings.TrimSpace(c.TagNumber)
		if label == "" {
			label = strings.TrimSpace(c.DisplayID)
		}
		if label == "" {
			// Never render a raw id to a person. A sibling queue once shipped
			// "Raised by <uuid>" to operators.
			label = "One animal"
		}
		parts = append(parts, label+" — "+c.BlockedReason)
	}
	return Unprocessable("animals_blocked",
		"These animals cannot be sold yet: "+strings.Join(parts, "; "))
}

// GetSaleAllocation reads back the animals one sale is made of, shed-wise.
//
// This is what lets the Sales page show a recorded deal's real animals: the sales module
// stores no goat_id, so the mapping is read from here (see migration 000177 for why the
// mapping lives on the herd side).
func (s *SaleAllocationService) GetSaleAllocation(ctx context.Context, tenantID, salesDealID string) ([]ports.SaleAllocationShedGroup, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	deal := strings.TrimSpace(salesDealID)
	if !uuidPattern.MatchString(deal) {
		return nil, BadRequest("invalid_sales_deal_id", "sales_deal_id must be a valid UUID")
	}
	groups, err := s.reader.ListSaleAllocations(ctx, tenant, deal)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if groups == nil {
		groups = []ports.SaleAllocationShedGroup{}
	}
	return groups, nil
}

// GetSaleLocations serves the picker's park/shed/pen vocabulary.
func (s *SaleAllocationService) GetSaleLocations(ctx context.Context, tenantID string) (*ports.SaleLocationCatalog, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	catalog, err := s.reader.ListSaleLocations(ctx, tenant)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return catalog, nil
}

// checkSaleCountGate refuses anything but an exact fill.
//
// The three refusals are deliberately separate sentences, because the next action differs:
// too few means keep picking, too many means unpick, and an already-complete sale means
// this deal is finished and the operator is on the wrong row.
func checkSaleCountGate(deal *ports.SaleDeal, selected int) error {
	remaining := deal.Remaining()
	if remaining == 0 {
		return Unprocessable("sale_already_mapped",
			fmt.Sprintf("This sale is already complete: all %d animals are tagged to it.",
				deal.DeclaredAnimalCount))
	}
	if selected < remaining {
		short := remaining - selected
		return Unprocessable("sale_not_fully_mapped",
			fmt.Sprintf("This sale is for %d animals and %d %s still to be picked. Pick all %d before confirming — a sale cannot be tagged half now and half later.",
				deal.DeclaredAnimalCount, short, plural(short, "is", "are"), remaining))
	}
	if selected > remaining {
		return Unprocessable("sale_over_mapped",
			fmt.Sprintf("This sale is for %d animals and you have picked %d. Remove %d.",
				deal.DeclaredAnimalCount, deal.AlreadyTagged+selected, selected-remaining))
	}
	return nil
}

// mapSaleDealErr turns the ledger read's failures into farm-worded refusals.
func mapSaleDealErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrSaleDealNotFound):
		return NotFound("that sale is not in the ledger")
	case errors.Is(err, ports.ErrSaleDealNoAnimalCount):
		return Unprocessable("sale_has_no_animal_count",
			"This sale does not say how many animals it is for, so animals cannot be tagged to it.")
	default:
		return mapRepoErr(err)
	}
}

// plural picks the verb for a count. Operator-facing copy: "1 is still to be picked"
// rather than "1 are", which reads as a machine talking.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
