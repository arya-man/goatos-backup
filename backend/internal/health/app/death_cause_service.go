package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// DeathCauseCatalogService serves the searchable disease list the death form's dropdown
// reads, and is the ONE place a submitted cause is checked against the clinical vocabulary.
//
// The vocabulary is the DIAGNOSIS REGISTER (maintainer decision 2026-09-05), not the
// published treatment protocols. The register is precise where the protocols are lossy:
// PPR, POX and UNDIFFERENTIATED all route to one 'supportive' card, so a cause recorded as
// a card key cannot say which of the three killed the animal. It is also the same key space
// `health_cases.register_rule_id` stores and the Health Analytics disease board counts, so
// a cause of death can be read straight against the incidence board.
//
// The catalog is built ONCE. The registers are embedded YAML validated at process start —
// they cannot change under a running process, so rebuilding the list per request would
// re-walk four rule tables to produce a byte-identical answer.
type DeathCauseCatalogService struct {
	once    sync.Once
	catalog domain.DeathCauseCatalog
	err     error
}

func NewDeathCauseCatalogService() *DeathCauseCatalogService { return &DeathCauseCatalogService{} }

// Catalog returns the whole searchable vocabulary.
func (s *DeathCauseCatalogService) Catalog(context.Context) (domain.DeathCauseCatalog, error) {
	s.once.Do(s.build)
	return s.catalog, s.err
}

// ValidateCause refuses a cause the register does not name.
//
// A cause that cannot be resolved is REJECTED rather than stored as typed: the whole value
// of a coded cause is that it groups, and one death filed under 'MASTITIS' beside another
// under a near-miss is two diseases on the board and one in the barn. An operator who
// cannot find the disease records a NORMAL death and says so in the note.
// The signature is STRING-SHAPED rather than domain-typed so the module that records
// deaths can hold a narrow port over it without importing health's types. Counts is that
// caller: it records that an animal died, and has no business knowing what diseases exist.
func (s *DeathCauseCatalogService) ValidateCause(ctx context.Context, key, kind string) error {
	catalog, err := s.Catalog(ctx)
	if err != nil {
		return err
	}
	return domain.ValidateDeathCause(domain.DeathCause{Key: key, Kind: kind}, catalog)
}

func (s *DeathCauseCatalogService) build() {
	rules := make([]domain.RegisterRule, 0, 128)
	versions := make([]string, 0, len(diagnosis.Classes))
	for _, class := range diagnosis.Classes {
		register, err := diagnosis.RegisterFor(class)
		if err != nil {
			// A register that will not load is a PROCESS-level fault, not a per-request
			// one: the engine diagnoses from these same files, so an unreadable register
			// means the deployment is broken. Failing the whole catalog is honest —
			// serving a partial disease list would silently narrow what an operator can
			// record a death as, with nothing on screen to say so.
			s.err = fmt.Errorf("health: load %s diagnosis register: %w", class, err)
			return
		}
		versions = append(versions, register.Version)
		for _, rule := range register.Rules {
			// ONLY a `problem` rule is a disease. The register also carries `field`
			// rules — TICKS, HOOF, ANTIHISTAMINE, SEPARATE_FEEDING — which are field
			// ACTIONS the engine can advise, not conditions. Offering "Separate feeding"
			// as a cause of death would be nonsense on the operator's screen and worse in
			// the mortality board.
			if rule.Kind != diagnosis.KindProblem {
				continue
			}
			rules = append(rules, domain.RegisterRule{
				ID:    rule.ID,
				Label: domain.DeathCauseLabel(rule.ID),
				Class: class,
			})
		}
	}
	s.catalog = domain.BuildDeathCauseCatalog(rules, versions)
}
