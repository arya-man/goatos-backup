package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// AN ITEM WRITE MUST NOT READ THE COUNTED CATEGORY PROJECTION (21ac4f56a).
//
// kindFor used to resolve an item's kind through Get(categories), and that read composes the
// category tree WITH the per-list item counts -- a full count of the list's items on every item
// WRITTEN. One drawer save does not notice; a lakh-row workbook does, and the observed import
// rate fell from ~190 to ~60 rows/s as the list filled. The store already had the recursive
// root-kind walk, so the write path asks for the kind and nothing else.
//
// WHY THIS IS SHAPED AS A CALL-COUNT AND NOT A TIMING. The defect is quadratic work, and the only
// honest small-fixture statement of "quadratic" is WHICH READ the write performs: the counted
// projection must not be on the write path at all. A duration assertion on a fake repository
// would measure nothing and would flake on a loaded machine.
//
// The commit that made this change shipped only a compile-stub CategoryKind on an existing fake,
// so reverting it left every test green -- one of the disproved rows in the revert receipts, and
// a real one. Mutation proof: restore `s.repo.Get(ctx, tenantID, RegCategories, ...)` in
// Service.kindFor and this goes red on the categoriesRead counter.
type kindCountingRepo struct {
	*applyCancelRepo
	categoriesRead int
	categoryKind   int
	kind           string
}

func (r *kindCountingRepo) Get(_ context.Context, _, register, id string) (domain.Row, error) {
	if register == domain.RegCategories {
		r.categoriesRead++
		return domain.Row{ID: id, Register: register, Fields: map[string]any{"kind": r.kind}}, nil
	}
	// The stored item the update is fenced against. It carries its own category_id so the
	// existing-row branch of kindFor is exercised too.
	return domain.Row{ID: id, Register: register, RowVersion: 1, Fields: map[string]any{"name": "Oxytet", "category_id": "cat-1", "unit": "ml"}}, nil
}

func (r *kindCountingRepo) CategoryKind(context.Context, string, string) (string, error) {
	r.categoryKind++
	return r.kind, nil
}

func newItemFields() map[string]any {
	// `unit` is required for a medicine, which is the point: the kind the write resolves is what
	// decides which fields are required, so a test that stopped at "no error" would also pass
	// with the kind resolved as "" and every kind-scoped rule skipped.
	return map[string]any{"name": "Oxytet", "category_id": "cat-1", "unit": "ml"}
}

func TestItemWriteResolvesKindWithoutTheCountedCategoryProjection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*Service, context.Context) error
	}{
		{"create", func(s *Service, ctx context.Context) error {
			_, err := s.Create(ctx, ports.WriteParams{TenantID: "t-1"}, domain.RegItems, newItemFields())
			return err
		}},
		{"update", func(s *Service, ctx context.Context) error {
			_, err := s.Update(ctx, ports.WriteParams{TenantID: "t-1"}, domain.RegItems, "item-1", newItemFields(), 1)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &kindCountingRepo{applyCancelRepo: &applyCancelRepo{}, kind: "medicine"}
			if err := tc.write(NewService(repo), context.Background()); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if repo.categoryKind == 0 {
				t.Errorf("%s never asked for the category's kind -- the cheap walk is off the write path", tc.name)
			}
			if repo.categoriesRead != 0 {
				t.Errorf("%s read the categories register %d time(s): that read counts every item of the list on every item written, which is the quadratic import this fix removed",
					tc.name, repo.categoriesRead)
			}
		})
	}
}

// A category that does not exist must still be refused, and refused as a FIELD error the drawer
// can show -- not as a 500 and not silently as the empty kind, which would skip every kind-scoped
// required field. The cheap walk reports absence with ErrNotFound; this pins the translation.
func TestUnknownCategoryIsAFieldErrorNotAMissingKind(t *testing.T) {
	repo := &kindNotFoundRepo{applyCancelRepo: &applyCancelRepo{}}
	_, err := NewService(repo).Create(context.Background(), ports.WriteParams{TenantID: "t-1"}, domain.RegItems, newItemFields())
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a validation error for an unknown category, got %v", err)
	}
	if len(ve.Fields) != 1 || ve.Fields[0].Field != "category_id" || ve.Fields[0].Code != "unknown" {
		t.Fatalf("the refusal must name category_id as unknown, got %+v", ve.Fields)
	}
}

type kindNotFoundRepo struct{ *applyCancelRepo }

func (r *kindNotFoundRepo) CategoryKind(context.Context, string, string) (string, error) {
	return "", ports.ErrNotFound
}
