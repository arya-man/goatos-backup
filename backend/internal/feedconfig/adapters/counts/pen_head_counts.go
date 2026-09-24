// Package counts answers Feed Config's pen head counts from the SAME projection and the SAME pen
// matching the feed sheet uses, so the "Animals in this pen" the author reads is the number the
// sheet multiplies their grams by (maintainer decision 2026-09-24).
//
// It issues no SQL of its own: the counts module owns the census, and feed direction's reader is
// the one adapter that already drains it for the sheet. Reusing that reader -- rather than a second
// census query here -- is what keeps the two numbers from drifting.
package counts

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	fddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// GrainReader is the feed sheet's own projected-grain read (feeddirection/adapters/counts.Reader).
type GrainReader interface {
	ProjectedGrainsForSheds(ctx context.Context, req fdports.ProjectedGrainsRequest) (map[string][]fddomain.ShedGrain, error)
}

// PenHeadCounts implements ports.PenHeadCounts over the feed sheet's grain reader.
type PenHeadCounts struct {
	grains GrainReader
}

func New(grains GrainReader) *PenHeadCounts { return &PenHeadCounts{grains: grains} }

var _ ports.PenHeadCounts = (*PenHeadCounts)(nil)

// ProjectedPenHeadCounts reads each park's grains ONCE, for every shed on the page as a set, and
// sums a pen's grains exactly as ExperimentPlanner.PlanDaily does: every grain whose
// PartitionMatchKey equals the pen's. One read per PARK (at most two today), never one per pen.
func (p *PenHeadCounts) ProjectedPenHeadCounts(ctx context.Context, tenantID string, pens []ports.PenRef, feedDay time.Time) ([]int64, error) {
	out := make([]int64, len(pens))
	if len(pens) == 0 {
		return out, nil
	}
	shedsByPark := map[string][]string{}
	seen := map[string]bool{}
	parks := []string{}
	for _, pen := range pens {
		if _, ok := shedsByPark[pen.ParkID]; !ok {
			parks = append(parks, pen.ParkID)
		}
		key := pen.ParkID + "|" + pen.ShedID
		if !seen[key] {
			seen[key] = true
			shedsByPark[pen.ParkID] = append(shedsByPark[pen.ParkID], pen.ShedID)
		}
	}
	totals := map[string]int64{}
	// scale-guard:ignore: one projection read per PARK in the page (two parks live), each carrying the page's sheds as a set; never one read per pen or shed.
	for _, parkID := range parks {
		grains, err := p.grains.ProjectedGrainsForSheds(ctx, fdports.ProjectedGrainsRequest{
			TenantID:   tenantID,
			ParkID:     parkID,
			TargetDate: feedDay,
			ShedIDs:    shedsByPark[parkID],
		})
		if err != nil {
			return nil, fmt.Errorf("feedconfig: projected pen head counts: %w", err)
		}
		for shedID, list := range grains {
			for _, grain := range list {
				totals[fddomain.ExperimentLocationKey(shedID, grain.PartitionLabel)] += grain.HeadCount
			}
		}
	}
	for i, pen := range pens {
		out[i] = totals[fddomain.ExperimentLocationKey(pen.ShedID, pen.PartitionLabel)]
	}
	return out, nil
}
