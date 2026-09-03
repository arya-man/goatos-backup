package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// loadwiseServedLoads bounds the load-wise read to the newest loads. The report is a chart plus a
// reconciliation table, not a pageable ledger; TotalLoads tells the screen when older loads exist
// beyond the served window.
const loadwiseServedLoads = 60

// LoadwiseService serves the Sales page's load-wise reconciliation and the load-cost entry.
//
// Thin on purpose, like FeedPurchaseService: a load's cost is a commercial record with no state
// machine, and the read is a reporting aggregate the repository owns end to end.
type LoadwiseService struct {
	repo ports.LoadwiseRepository
}

func NewLoadwiseService(repo ports.LoadwiseRepository) *LoadwiseService {
	return &LoadwiseService{repo: repo}
}

// LoadwiseSales returns the load-wise reconciliation read, optionally narrowed to one park. A
// present-but-malformed park id is rejected rather than silently served unfiltered — a filter
// that quietly falls back to All Parks is the exact defect the park parameter exists to fix.
func (s *LoadwiseService) LoadwiseSales(ctx context.Context, tenantID, parkID string) (domain.LoadwiseSales, error) {
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.LoadwiseSales{}, domain.ErrLoadwiseValidation{Field: "park_id", Reason: "is not a park this farm knows"}
	}
	return s.repo.LoadwiseSales(ctx, tenantID, parkID, loadwiseServedLoads)
}

// SetLoadCost validates and records (or clears) one load's landed cost.
func (s *LoadwiseService) SetLoadCost(ctx context.Context, tenantID, loadID string, edit domain.LoadCostEdit, actorID string) error {
	if strings.TrimSpace(loadID) == "" {
		return ports.ErrLoadNotFound
	}
	if err := edit.Validate(); err != nil {
		return err
	}
	return s.repo.SetLoadCost(ctx, tenantID, loadID, edit, actorID)
}

// loadwiseFieldLabel names the cost form's boxes for validation messages.
func loadwiseFieldLabel(field string) string {
	switch field {
	case "animal_cost":
		return "Animal cost"
	case "transport_cost":
		return "Transport cost"
	case "other_cost":
		return "Other cost"
	default:
		return "That field"
	}
}

// LoadwiseHTTPError maps a load-wise error onto the transport error shape. Same rule as
// FeedPurchaseHTTPError: every branch is a message a person can act on, and an unrecognised error
// falls through to a generic 500 rather than echoing internals.
func LoadwiseHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrLoadNotFound):
		return NotFound("Load not found.")
	default:
		var v domain.ErrLoadwiseValidation
		if errors.As(err, &v) {
			return &Error{
				Code:       "load_cost_invalid_" + v.Field,
				Message:    loadwiseFieldLabel(v.Field) + " " + v.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		return Internal("That could not be loaded. Try again.")
	}
}
