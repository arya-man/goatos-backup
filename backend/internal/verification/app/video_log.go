package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const (
	// defaultVideoLogRowLimit bounds ONE shed's day. A shed rarely carries more than a few dozen
	// pieces of filmed work, but a vaccination drive raises one item per animal, and a single shed
	// can hold several hundred -- so the limit is real, not theoretical.
	defaultVideoLogRowLimit = 200
	maxVideoLogRowLimit     = 500
	// The whole-day EXPORT is allowed a much larger bound than the screen: it is a file, read once,
	// and the honest answer to "give me today" is the whole day. It is still bounded -- an
	// unbounded row read is the anti-pattern this design avoids -- and a day that exceeds it comes
	// back flagged truncated rather than silently short.
	//
	// 20k covers a heavy park-day with room to spare (a busy day in this tenant is ~1k proofs
	// across ~74 sheds) without letting one request stream an unbounded result set.
	maxVideoLogExportLimit = 20000
)

// VideoLog serves the per-shed arrival log for ONE business day (domain.VideoLog).
//
// Authorization is the route-permission table's job (permissions.VerificationEvidenceTimeline on
// GET /verification/video-log), matching every other endpoint in this module. What this method DOES
// enforce is scope and input shape: the caller's authorized park set clamps the read, and a
// malformed or future business date is refused rather than silently coerced.
func (s *Service) VideoLog(ctx context.Context, params ports.VideoLogParams) (domain.VideoLog, error) {
	params.TenantID = strings.TrimSpace(params.TenantID)
	if !uuidutil.IsUUIDString(params.TenantID) {
		return domain.VideoLog{}, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}

	todayStart := biztime.BusinessDayStart(s.now())
	params.BusinessDate = strings.TrimSpace(params.BusinessDate)
	if params.BusinessDate == "" {
		// No day asked for means today. The log is a day view; there is no "all days" reading of it.
		params.BusinessDate = biztime.BusinessDate(s.now())
	}
	parsed, err := time.ParseInLocation("2006-01-02", params.BusinessDate, biztime.DefaultLocation())
	if err != nil {
		return domain.VideoLog{}, BadRequest("invalid_business_date", "business_date must be YYYY-MM-DD")
	}
	// A future day cannot have arrivals, and accepting one would render an empty log that reads as
	// "nothing was filmed" rather than "that day has not happened".
	if parsed.After(todayStart) {
		return domain.VideoLog{}, BadRequest("future_business_date", "business_date cannot be in the future")
	}
	params.BusinessDate = parsed.Format("2006-01-02")

	params.ParkID = strings.TrimSpace(params.ParkID)
	if params.ParkID != "" && !uuidutil.IsUUIDString(params.ParkID) {
		return domain.VideoLog{}, BadRequest("invalid_park", "park_id must be a UUID")
	}
	// A park-scoped caller with NO authorized parks can see nothing. Returning an empty log rather
	// than every park is the fail-closed reading; the repository's clamp would otherwise be a no-op
	// against an empty array in some drivers.
	if params.ScopeRestricted && len(params.ParkIDs) == 0 {
		return domain.VideoLog{BusinessDate: params.BusinessDate, Sheds: []domain.VideoLogShed{}}, nil
	}

	rowCeiling := maxVideoLogRowLimit
	if params.AllSheds {
		rowCeiling = maxVideoLogExportLimit
	}
	switch {
	case params.Limit <= 0:
		params.Limit = defaultVideoLogRowLimit
		if params.AllSheds {
			// An export that quietly defaulted to 200 rows would hand someone a file that looks
			// like a whole day and is not one.
			params.Limit = rowCeiling
		}
	case params.Limit > rowCeiling:
		params.Limit = rowCeiling
	}

	out := domain.VideoLog{BusinessDate: params.BusinessDate}

	sheds, err := s.repo.VideoLogShedSummary(ctx, params)
	if err != nil {
		return domain.VideoLog{}, err
	}
	nav := s.sourceModuleNavigation()
	for i := range sheds {
		// The summary groups by the raw module CODE stored on the item ("feed"). A code is config
		// vocabulary and the copy firewall bans it from visible UI, so it is replaced here with the
		// registry's display label. A module the registry does not know is DROPPED rather than
		// printed raw -- the shed's counts are unaffected, and a screen never shows "feed".
		labels := make([]string, 0, len(sheds[i].Modules))
		seen := make(map[string]struct{}, len(sheds[i].Modules))
		for _, module := range sheds[i].Modules {
			label := strings.TrimSpace(nav[module].label)
			if label == "" {
				continue
			}
			if _, dup := seen[label]; dup {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
		sort.Strings(labels)
		sheds[i].Modules = labels
	}
	if sheds == nil {
		sheds = []domain.VideoLogShed{}
	}
	out.Sheds = sheds

	params.ShedID = strings.TrimSpace(params.ShedID)
	// Rows are read for a SELECTED SHED (the panel's detail level) or for the WHOLE DAY (the CSV
	// export), never for the ordinary summary. AllSheds wins: an export asks for the day, not for
	// whichever shed happened to be open behind the dialog.
	if params.AllSheds {
		params.ShedID = ""
	} else if params.ShedID == "" {
		return out, nil
	}
	out.SelectedShedID = params.ShedID

	rows, truncated, err := s.repo.VideoLogShedRows(ctx, params)
	if err != nil {
		return domain.VideoLog{}, err
	}
	for i := range rows {
		s.decorateVideoLogRow(&rows[i], nav)
	}
	if rows == nil {
		rows = []domain.VideoLogRow{}
	}
	out.Rows = rows
	out.RowsTruncated = truncated
	return out, nil
}

// decorateVideoLogRow resolves every backend-owned label on one row: the module and category
// display copy, the queue's nav-module key, and each proof's header.
//
// The proof header resolution order matters and mirrors what the verifier's own drawer does:
//
//  1. the artifact's OWN verification_label metadata, written by the producer when it uploaded --
//     the richest answer, because it can name workflow task truth the registry cannot;
//  2. otherwise the category registry's MediaLabelFor, resolved from the proof's DECLARED ORDINAL.
//
// Ordinal, not slice index. A ref whose artifact is missing is absent from Proofs, and using the
// index would shift every later proof's label by one -- so a feed distribution item missing its
// weight photo would relabel its distribution video as the weight photo.
func (s *Service) decorateVideoLogRow(row *domain.VideoLogRow, nav map[string]moduleNavigation) {
	row.ModuleLabel = nav[row.Module].label
	row.NavModule = nav[row.Module].navModule
	def, known := s.registry.Get(row.Category)
	if known {
		row.CategoryLabel = strings.TrimSpace(def.PageLabel)
	}
	// mediaCount is the item's DECLARED proof count where the registry knows it, so a numbered
	// fallback label ("Video 2 of 5") counts against what the category expects rather than against
	// however many happened to resolve.
	mediaCount := len(row.Proofs)
	if known && len(def.ExpectedMedia) > mediaCount {
		mediaCount = len(def.ExpectedMedia)
	}
	for i := range row.Proofs {
		if strings.TrimSpace(row.Proofs[i].Label) != "" {
			continue
		}
		if !known {
			continue
		}
		row.Proofs[i].Label = def.MediaLabelFor(row.Proofs[i].Ordinal-1, mediaCount)
	}
}
