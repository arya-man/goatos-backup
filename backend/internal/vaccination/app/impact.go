package app

import (
	"context"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ImpactPreview computes a live impact preview for a vaccination rule/version: eligible goats,
// catch-up count, obligations, estimated drive batches, doses required vs available, and warnings
// (stock shortage, expiry-before-horizon). Real counts replace the mock's fake math. Overlapping
// published-window conflicts are enforced at publish time by the DB EXCLUDE constraint.
func (s *Service) ImpactPreview(ctx context.Context, req domain.ImpactRequest) (domain.ImpactPreview, error) {
	filter := req.Filter
	if filter.AsOf.IsZero() {
		filter.AsOf = req.AsOf
	}
	if filter.AsOf.IsZero() {
		filter.AsOf = time.Now().In(indiaLocation)
	}

	eligible, err := s.repo.CountEligibleGoats(ctx, filter)
	if err != nil {
		return domain.ImpactPreview{}, err
	}
	catchup, err := s.repo.CountCatchupGoats(ctx, filter)
	if err != nil {
		return domain.ImpactPreview{}, err
	}
	sheds, err := s.repo.CountEligibleShedScopes(ctx, filter)
	if err != nil {
		return domain.ImpactPreview{}, err
	}

	doseRows := req.DoseRows
	if doseRows < 1 {
		doseRows = 1
	}
	dosesPer := req.DosesPerGoat
	if dosesPer < 1 {
		dosesPer = 1
	}
	batches := sheds
	if batches == 0 && eligible > 0 {
		batches = 1 // eligible goats with no shed assignment still form one catch-up drive
	}

	out := domain.ImpactPreview{
		EligibleGoats: eligible,
		CatchupGoats:  catchup,
		Obligations:   eligible * int64(doseRows),
		Batches:       batches,
		DosesRequired: eligible * int64(dosesPer),
	}

	if req.VaccineItemID != nil && *req.VaccineItemID != "" {
		available, earliest, err := s.repo.SumAvailableStock(ctx, filter.TenantID, *req.VaccineItemID, req.LocationID)
		if err != nil {
			return domain.ImpactPreview{}, err
		}
		out.DosesAvailable = available
		out.EarliestExpiry = earliest
		if availNum, ok := parseNumeric(available); ok && float64(out.DosesRequired) > availNum {
			out.Warnings = append(out.Warnings,
				"stock shortage: required "+strconv.FormatInt(out.DosesRequired, 10)+" > available "+available)
		}
		if earliest != nil && req.HorizonDays > 0 {
			if earliest.Before(filter.AsOf.AddDate(0, 0, req.HorizonDays)) {
				out.Warnings = append(out.Warnings, "stock expiry before horizon: earliest "+earliest.Format("2006-01-02"))
			}
		}
	} else {
		out.Warnings = append(out.Warnings, "no vaccine item set — stock not evaluated")
	}

	return out, nil
}

func parseNumeric(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
