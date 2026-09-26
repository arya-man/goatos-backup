package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
		GoatID:    row.GoatID,
		DisplayID: row.DisplayID,
		TagNumber: row.TagNumber,
		// Never repeat one number as if it were two: an animal whose identifiers hold the
		// same string reads as a single tag, which is what it is.
		SecondaryTagNumber: secondaryTag(row.TagNumber, row.SecondaryTagNumber),
		ParkID:             row.ParkID,
		ParkName:           row.ParkName,
		ShedID:             row.ShedID,
		ShedName:           row.ShedName,
		PartitionLabel:     row.PartitionLabel,
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

// secondaryTag returns the animal's other identifier, blank when it is absent or is the
// same number the primary already shows.
func secondaryTag(primary, secondary string) string {
	secondary = strings.TrimSpace(secondary)
	if secondary == "" || strings.EqualFold(secondary, strings.TrimSpace(primary)) {
		return ""
	}
	return secondary
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
	// AllowedParkIDs is the caller's park scope as the handler resolved it: nil for a
	// tenant-wide caller, otherwise the parks they may act in. A park head asking about
	// another park is refused with park_out_of_scope rather than shown its animals.
	AllowedParkIDs []string
}

// parkAllowed answers whether parkID sits inside the resolved scope. nil is tenant-wide.
func parkAllowed(allowed []string, parkID string) bool {
	if allowed == nil {
		return true
	}
	for _, id := range allowed {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(parkID)) {
			return true
		}
	}
	return false
}

// refuseAnimalsOutsideScope is the same clamp on the preview and confirm: every named animal
// that exists must stand in a park the caller may act in. A missing animal has no park and is
// left to the gate, which already refuses it as not-in-the-herd.
func refuseAnimalsOutsideScope(allowed []string, candidates []ports.SaleCandidate) error {
	if allowed == nil {
		return nil
	}
	for _, c := range candidates {
		if c.ParkID == "" {
			continue
		}
		if !parkAllowed(allowed, c.ParkID) {
			return Forbidden("park_out_of_scope", "One or more of these animals stand in a park you do not work in. Tag only animals from your own park.")
		}
	}
	return nil
}

// scopeFarms turns a park scope into the FARM CODES the sales ledger names a sale by (a park's
// location_code IS a deal's farm). nil for a tenant-wide caller; an EMPTY, non-nil slice when
// the scope resolves to no farm, which every caller reads as "nothing is yours".
func (s *SaleAllocationService) scopeFarms(ctx context.Context, tenantID string, allowed []string) ([]string, error) {
	if allowed == nil {
		return nil, nil
	}
	catalog, err := s.reader.ListSaleLocations(ctx, tenantID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	farms := []string{}
	for _, park := range catalog.Parks {
		if parkAllowed(allowed, park.ParkID) && strings.TrimSpace(park.Label) != "" {
			farms = append(farms, strings.TrimSpace(park.Label))
		}
	}
	return farms, nil
}

// refuseSaleOutsideScope clamps the SALE, not only its animals: a park-scoped caller may act on
// a sale recorded at one of their own parks and on no other. Without it a park head could tag
// animals from their own pen onto another park's sale, or read back any sale's tags and weights by
// id. A tenant-wide caller is never looked up.
func (s *SaleAllocationService) refuseSaleOutsideScope(ctx context.Context, tenantID, dealID string, allowed []string) error {
	if allowed == nil {
		return nil
	}
	farms, err := s.scopeFarms(ctx, tenantID, allowed)
	if err != nil {
		return err
	}
	farm, err := s.deals.ReadSaleDealFarm(ctx, tenantID, dealID)
	if err != nil {
		return mapSaleDealErr(err)
	}
	for _, f := range farms {
		if strings.EqualFold(f, farm) {
			return nil
		}
	}
	return Forbidden("park_out_of_scope", "This sale was recorded at a park you do not work in. Tag only the sales of your own park.")
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
	if parkID != "" && !parkAllowed(input.AllowedParkIDs, parkID) {
		return nil, nil, Forbidden("park_out_of_scope", "That park is not one you work in. Pick animals from your own park.")
	}
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
	today := biztime.BusinessDate(time.Now().In(biztime.DefaultLocation()))
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
	// AllowedParkIDs is the caller's resolved park scope; nil is tenant-wide. See
	// ListSaleCandidatesInput.
	AllowedParkIDs []string
}

// PreviewSaleAllocation is the review step: what you picked, grouped shed-wise, with the
// refused animals and their reasons. It MUTATES NOTHING.
func (s *SaleAllocationService) PreviewSaleAllocation(ctx context.Context, input PreviewSaleAllocationInput) (*ports.SaleAllocationPreview, error) {
	tenantID, dealID, goatIDs, err := validateAllocationInput(input.TenantID, input.SalesDealID, input.GoatIDs)
	if err != nil {
		return nil, err
	}
	if err := s.refuseSaleOutsideScope(ctx, tenantID, dealID, input.AllowedParkIDs); err != nil {
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
	if err := refuseAnimalsOutsideScope(input.AllowedParkIDs, candidates); err != nil {
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
	// AnimalWeightsKg is the live weight of every picked animal, keyed by goat id, as the
	// operator typed it at tagging (maintainer decision 2026-09-08). REQUIRED for every animal
	// in GoatIDs: a sale weight is what the Sales page's weight bands are made of, and an
	// animal tagged without one would be sold with no record of what left.
	AnimalWeightsKg map[string]string
	// AllowedParkIDs is the caller's resolved park scope; nil is tenant-wide. See
	// ListSaleCandidatesInput.
	AllowedParkIDs []string
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
	weights, err := validateAnimalWeights(goatIDs, input.AnimalWeightsKg)
	if err != nil {
		return nil, err
	}
	if err := s.refuseSaleOutsideScope(ctx, tenantID, dealID, input.AllowedParkIDs); err != nil {
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
	if err := refuseAnimalsOutsideScope(input.AllowedParkIDs, candidates); err != nil {
		return nil, err
	}
	blocked := make([]ports.SaleCandidate, 0)
	rows := make([]ports.SaleAllocationRow, 0, len(candidates))
	for _, c := range candidates {
		if !c.Sellable {
			blocked = append(blocked, c)
			continue
		}
		rows = append(rows, ports.SaleAllocationRow{GoatID: c.GoatID, RowVersion: c.RowVersion, WeightKg: weights[c.GoatID]})
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
		OccurredAt:           time.Now(),
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
	today := biztime.BusinessDate(time.Now().In(biztime.DefaultLocation()))
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

// saleWeightPattern is a positive live weight with at most two decimals (numeric(7,2)): up to
// five whole digits, far beyond any animal, so the bound only mirrors storage.
var saleWeightPattern = regexp.MustCompile(`^\d{1,5}(\.\d{1,2})?$`)

// validateAnimalWeights checks that EVERY de-duplicated picked animal carries a usable weight
// (maintainer decision 2026-09-08: the weight at tagging is required) and returns them trimmed,
// keyed by goat id. A missing or malformed one refuses the whole confirm BEFORE the herd is
// re-judged, so nothing is read or written for a request the operator must complete first.
func validateAnimalWeights(goatIDs []string, weights map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(goatIDs))
	for _, id := range goatIDs {
		raw := strings.TrimSpace(weights[id])
		if raw == "" {
			return nil, BadRequest("weight_required", "enter a weight in kg for every animal before confirming")
		}
		if !saleWeightPattern.MatchString(raw) || strings.Trim(raw, "0.") == "" {
			return nil, BadRequest("invalid_weight", "every weight must be a number of kg more than zero, up to two decimals")
		}
		out[id] = raw
	}
	return out, nil
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
//
// allowed is the caller's park scope (nil = tenant-wide); a park-scoped caller is refused a
// sale recorded at another park.
func (s *SaleAllocationService) GetSaleAllocation(ctx context.Context, tenantID, salesDealID string, allowed []string) ([]ports.SaleAllocationShedGroup, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	deal := strings.TrimSpace(salesDealID)
	if !uuidPattern.MatchString(deal) {
		return nil, BadRequest("invalid_sales_deal_id", "sales_deal_id must be a valid UUID")
	}
	if err := s.refuseSaleOutsideScope(ctx, tenant, deal, allowed); err != nil {
		return nil, err
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

// GetSaleAllocationAnimals reads back the animals one sale is made of, one per row, with the
// weight recorded for each. Same deal-id rules as GetSaleAllocation.
func (s *SaleAllocationService) GetSaleAllocationAnimals(ctx context.Context, tenantID, salesDealID string, allowed []string) ([]ports.SaleAllocationAnimal, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	deal := strings.TrimSpace(salesDealID)
	if !uuidPattern.MatchString(deal) {
		return nil, BadRequest("invalid_sales_deal_id", "sales_deal_id must be a valid UUID")
	}
	if err := s.refuseSaleOutsideScope(ctx, tenant, deal, allowed); err != nil {
		return nil, err
	}
	animals, err := s.reader.ListSaleAllocationAnimals(ctx, tenant, deal)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if animals == nil {
		animals = []ports.SaleAllocationAnimal{}
	}
	return animals, nil
}

// ListSaleTaggingQueueInput scopes the park head's queue.
type ListSaleTaggingQueueInput struct {
	TenantID string
	// AllowedParkIDs is the caller's resolved park scope; nil is tenant-wide. The queue is
	// narrowed to the FARM CODES of those parks, because the sales ledger names a sale's
	// farm by code (CBE, CPT), never by park id.
	AllowedParkIDs []string
	Limit          int
	Cursor         string
}

// ListSaleTaggingQueue is the tag-only list (maintainer decision 2026-09-11): the sales at the
// caller's park that still owe animals. A park-scoped caller whose parks resolve to no farm
// code sees an EMPTY queue rather than every farm's -- failing closed is the whole point of a
// scope.
func (s *SaleAllocationService) ListSaleTaggingQueue(ctx context.Context, input ListSaleTaggingQueueInput) ([]ports.SaleTaggingDeal, *string, error) {
	tenant := strings.TrimSpace(input.TenantID)
	if tenant == "" {
		return nil, nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	farms, err := s.scopeFarms(ctx, tenant, input.AllowedParkIDs)
	if err != nil {
		return nil, nil, err
	}
	if farms != nil && len(farms) == 0 {
		return []ports.SaleTaggingDeal{}, nil, nil
	}
	limit := input.Limit
	if limit <= 0 || limit > ports.SaleTaggingQueuePageSize {
		limit = ports.SaleTaggingQueuePageSize
	}
	deals, cursor, err := s.deals.ListSaleTaggingDeals(ctx, tenant, farms, limit, strings.TrimSpace(input.Cursor))
	if err != nil {
		return nil, nil, mapSaleDealErr(err)
	}
	if deals == nil {
		deals = []ports.SaleTaggingDeal{}
	}
	return deals, cursor, nil
}

// GetSaleLocations serves the picker's park/shed/pen vocabulary.
//
// allowed is the caller's park scope (nil = tenant-wide): a park-scoped caller is served their
// own parks and pens only, so the phone never offers a park the picker would then refuse.
func (s *SaleAllocationService) GetSaleLocations(ctx context.Context, tenantID string, allowed []string) (*ports.SaleLocationCatalog, error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		return nil, BadRequest("missing_tenant", "tenant scope is required")
	}
	catalog, err := s.reader.ListSaleLocations(ctx, tenant)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if allowed == nil || catalog == nil {
		return catalog, nil
	}
	out := &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{}, Locations: []ports.SaleLocationEntry{}}
	for _, p := range catalog.Parks {
		if parkAllowed(allowed, p.ParkID) {
			out.Parks = append(out.Parks, p)
		}
	}
	for _, l := range catalog.Locations {
		if parkAllowed(allowed, l.ParkID) {
			out.Locations = append(out.Locations, l)
		}
	}
	return out, nil
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
	case errors.Is(err, ports.ErrSaleDealFailed):
		return saleDealFailedConflict()
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

// saleDealFailedConflict refuses tagging onto a sale marked failed (maintainer decision
// 2026-09-25): tagging exits the animals as sold, and a failed sale sold nothing.
func saleDealFailedConflict() *Error {
	return Conflict("sale_deal_failed", "This sale is marked failed, so animals cannot be tagged to it.")
}
