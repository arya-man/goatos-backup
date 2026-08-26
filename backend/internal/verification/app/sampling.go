package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// RANDOMIZED VERIFICATION SAMPLING -- use-cases (maintainer decision 2026-08-26).
//
// The CEO sets, per category, what percentage of that category's proof videos the verifier must
// watch; the rest are settled by the policy. Everything visible here is composed from the
// REGISTRY, so a producer that registers a category gets a Randomization row with no change to
// this file, and no category token ever reaches a screen.

// defaultSamplingSettleLimit bounds one closeout pass. The stage runs on the 5-minute operational
// lane, so a large backlog drains over several ticks rather than in one long transaction.
const defaultSamplingSettleLimit = 100

// SamplingOverview composes the Randomization panel for one Asia/Kolkata business date: every
// registered category, the percentage in force that day, and how the day is going against it.
//
// An empty businessDate means today. A FUTURE date is refused rather than clamped: a day that has
// not happened has no capture to report, and silently answering about today would make the panel
// disagree with the date printed above it.
func (s *Service) SamplingOverview(ctx context.Context, tenantID, businessDate string) (domain.SamplingOverview, error) {
	tenantID = strings.TrimSpace(tenantID)
	if !uuidutil.IsUUIDString(tenantID) {
		return domain.SamplingOverview{}, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	businessDate, err := s.resolveSamplingBusinessDate(businessDate)
	if err != nil {
		return domain.SamplingOverview{}, err
	}
	policies, err := s.repo.ListSamplingPolicies(ctx, tenantID, businessDate)
	if err != nil {
		return domain.SamplingOverview{}, mapRepoErr(err)
	}
	byCategory := make(map[string]ports.SamplingPolicyRow, len(policies))
	for _, row := range policies {
		byCategory[row.Category] = row
	}
	stats, err := s.repo.ListSamplingDayStats(ctx, tenantID, businessDate)
	if err != nil {
		return domain.SamplingOverview{}, mapRepoErr(err)
	}

	definitions := s.registry.List()
	rows := make([]domain.SamplingCategory, 0, len(definitions))
	for _, def := range definitions {
		if def.NavigationModule == "" || def.PageLabel == "" {
			// A category with no navigation copy has no name a CEO could read, and the copy
			// firewall bans showing its raw token. Registering that copy is what puts a module on
			// this panel.
			continue
		}
		row := domain.SamplingCategory{
			Category:      def.Category,
			ModuleKey:     def.NavigationModule,
			ModuleLabel:   def.NavigationModuleLabel,
			PageLabel:     def.PageLabel,
			PageOrder:     def.PageOrder,
			SamplePercent: domain.DefaultSamplePercent,
			Waivable:      def.SamplingWaivable(),
		}
		if !row.Waivable {
			// Locked AT 100 regardless of any stored row: the verifier is this category's data
			// source, not a spot check. Reporting the stored number here would advertise a
			// sampling rate the queue does not apply.
			row.LockedReason = domain.SamplingLockedReason
		} else if policy, ok := byCategory[def.Category]; ok {
			row.SamplePercent = policy.Percent
			row.EffectiveFrom = policy.EffectiveBusinessDate
			row.SetByName = policy.SetByName
			if !policy.SetAt.IsZero() {
				row.SetAt = policy.SetAt.UTC().Format(time.RFC3339)
			}
		}
		row.Stats = stats[def.Category]
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ModuleLabel != rows[j].ModuleLabel {
			return rows[i].ModuleLabel < rows[j].ModuleLabel
		}
		if rows[i].PageOrder != rows[j].PageOrder {
			return rows[i].PageOrder < rows[j].PageOrder
		}
		return rows[i].PageLabel < rows[j].PageLabel
	})
	return domain.SamplingOverview{BusinessDate: businessDate, Categories: rows}, nil
}

// SetSamplingPolicy records one category's percentage, effective from TODAY's business date.
//
// The effective date is the SERVER's, never the client's: a caller that could name its own date
// could rewrite a day the verifier has already worked and retroactively change what she owed.
// Earlier days keep their own rows and therefore their own percentage.
func (s *Service) SetSamplingPolicy(ctx context.Context, in domain.SetSamplingPolicy) (domain.SamplingCategory, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Category = strings.TrimSpace(in.Category)
	in.ActorID = strings.TrimSpace(in.ActorID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return domain.SamplingCategory{}, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	def, ok := s.registry.Get(in.Category)
	if !ok {
		return domain.SamplingCategory{}, BadRequest("unknown_category", "category is not registered")
	}
	if !def.SamplingWaivable() {
		// Fail CLOSED and say why. Accepting the number and quietly ignoring it would show the CEO
		// a 40% he believes is in force while every video still reaches the verifier.
		return domain.SamplingCategory{}, BadRequest("sampling_not_available", domain.SamplingLockedReason)
	}
	// PRESENT-BUT-OUT-OF-RANGE IS A REFUSAL, never a silent default (the authored-config rule): an
	// operator who typed 140 must be told, not quietly given 100.
	if in.Percent < 0 || in.Percent > 100 {
		return domain.SamplingCategory{}, BadRequest("invalid_sample_percent", "sample_percent must be between 0 and 100")
	}
	in.EffectiveBusinessDate = biztime.BusinessDate(s.now())
	// DERIVED, not random, when the caller sent no key: the same (category, day, share) is the same
	// logical act, so a double-click or a retried Server Action is ONE write. It is self-expiring --
	// a genuinely different share the next day, or a different value today, is a different key and
	// is never mistaken for a replay of this one.
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		in.IdempotencyKey = fmt.Sprintf("verification-sampling-%s-%s-%d", in.Category, in.EffectiveBusinessDate, in.Percent)
	}
	if err := s.repo.UpsertSamplingPolicy(ctx, in); err != nil {
		return domain.SamplingCategory{}, mapRepoErr(err)
	}
	overview, err := s.SamplingOverview(ctx, in.TenantID, in.EffectiveBusinessDate)
	if err != nil {
		return domain.SamplingCategory{}, err
	}
	for _, row := range overview.Categories {
		if row.Category == in.Category {
			return row, nil
		}
	}
	return domain.SamplingCategory{}, BadRequest("unknown_category", "category is not registered")
}

// SettleUnsampledItems is the closeout the kernel stage runs: approve the pending items of CLOSED
// business days the policy did not draw, so no producer's workflow waits forever on a review that
// is never going to happen.
//
// It runs on a cadence rather than at enqueue because the percentage stays editable for the whole
// of the current business day -- see ports.SettleUnsampledParams.Before.
func (s *Service) SettleUnsampledItems(ctx context.Context, tenantID string, limit int) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	if !uuidutil.IsUUIDString(tenantID) {
		return 0, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	if limit <= 0 {
		limit = defaultSamplingSettleLimit
	}
	settled, err := s.repo.SettleUnsampledItems(ctx, ports.SettleUnsampledParams{
		TenantID:           tenantID,
		Before:             biztime.BusinessDayStart(s.now()),
		WaivableCategories: s.waivableCategories(),
		Limit:              limit,
	})
	if err != nil {
		return 0, mapRepoErr(err)
	}
	return settled, nil
}

// waivableCategories is the registry-derived allowlist for the closeout. A category whose approve
// must carry a measurement is excluded, so its unsampled items are left for a human rather than
// approved with the number missing.
func (s *Service) waivableCategories() []string {
	definitions := s.registry.List()
	out := make([]string, 0, len(definitions))
	for _, def := range definitions {
		if def.SamplingWaivable() {
			out = append(out, def.Category)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) resolveSamplingBusinessDate(businessDate string) (string, error) {
	businessDate = strings.TrimSpace(businessDate)
	today := biztime.BusinessDayStart(s.now())
	if businessDate == "" {
		return biztime.BusinessDate(s.now()), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return "", BadRequest("invalid_business_date", "business_date must be YYYY-MM-DD")
	}
	if parsed.After(today) {
		return "", BadRequest("future_business_date", "business_date cannot be in the future")
	}
	return businessDate, nil
}
