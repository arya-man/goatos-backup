package app

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// ErrMortalityUnavailable is returned when GetMortality is called on a HerdRegisterService
// whose wired repo does not implement ports.MortalityRepository. Fails closed on purpose:
// an empty payload would render as a farm where no animal has died, which is a different
// claim from "this read is not wired here".
var ErrMortalityUnavailable = errors.New("counts: mortality repository is not configured")

// WithDeathCauseLabeler supplies Health's cause-of-death vocabulary so the cause series
// carries the disease's farm name rather than its register key. Without it a recorded
// cause is shown under its key, humanised -- never dropped.
func (s *HerdRegisterService) WithDeathCauseLabeler(labeler ports.DeathCauseLabeler) *HerdRegisterService {
	s.deathCauses = labeler
	return s
}

// GetMortality returns the Counts -> Mortality payload.
//
// The adapter's CAUSE contract, which this layer completes:
//
//	basis "recorded"  Key is the raw register key, Label is EMPTY -- resolved here through
//	                  the Health port, so the postgres adapter never imports another
//	                  module's vocabulary.
//	basis "inferred"  Key is "inferred:<disease name>", Label is the case's own disease
//	                  name, already farm copy.
//	basis "none"      Key is "", Label takes the domain's one owned label.
//
// The same scheme rides the cause column of the three cross tabs and the CauseLabel of each
// row in the recent list.
func (s *HerdRegisterService) GetMortality(ctx context.Context, req domain.MortalityQuery) (domain.Mortality, error) {
	if s.mortality == nil {
		return domain.Mortality{}, ErrMortalityUnavailable
	}
	out, err := s.mortality.GetMortality(ctx, req)
	if err != nil {
		return domain.Mortality{}, err
	}
	resolve := func(key string) string {
		if s.deathCauses != nil {
			if label := s.deathCauses.LabelDeathCause(ctx, req.TenantID, key); label != "" {
				return label
			}
		}
		return key
	}
	for i := range out.Cause {
		b := &out.Cause[i]
		switch b.Basis {
		case domain.MortalityCauseRecorded:
			if b.Label == "" {
				b.Label = resolve(b.Key)
			}
		case domain.MortalityCauseNone:
			b.Label = domain.MortalityCauseNoneLabel
		}
	}
	relabel := func(cells []domain.MortalityCrossCell) {
		for i := range cells {
			c := &cells[i]
			switch {
			case c.ColKey == "":
				c.ColLabel = domain.MortalityCauseNoneLabel
			case c.ColLabel == "":
				c.ColLabel = resolve(c.ColKey)
			}
		}
	}
	relabel(out.LoadByCause)
	relabel(out.VendorByCause)
	relabel(out.BreedByCause)
	for i := range out.Deaths {
		d := &out.Deaths[i]
		switch d.CauseBasis {
		case domain.MortalityCauseRecorded:
			if d.CauseLabel == "" {
				d.CauseLabel = resolve(d.CauseKey)
			}
		case domain.MortalityCauseNone:
			d.CauseLabel = domain.MortalityCauseNoneLabel
		}
	}
	return out, nil
}
