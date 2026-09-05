package app

import (
	"context"
	"fmt"
	"strings"
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
func (s *DeathCauseCatalogService) ValidateCause(ctx context.Context, cause domain.DeathCause) error {
	catalog, err := s.Catalog(ctx)
	if err != nil {
		return err
	}
	return domain.ValidateDeathCause(cause, catalog)
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
				Label: deathCauseLabel(rule.ID),
				Class: class,
			})
		}
	}
	s.catalog = domain.BuildDeathCauseCatalog(rules, versions)
}

// deathCauseLabels is the farm's word for each diagnosis rule.
//
// WHY THIS MAP EXISTS AND `sop_ref` DOES NOT SERVE. The register's `sop_ref` names the
// TREATMENT SOP, not the disease: PPR, POX and UNDIFFERENTIATED all carry `sop_ref:
// Supportive`, so a dropdown built from it would offer the operator three different
// diseases under one identical label and give the mortality board no way to tell them
// apart. The rule ID is unique and precise but is a MACHINE KEY — 'FOOT_ROT' on a screen
// is the copy-firewall break this product bans — so the two are mapped here, in one
// reviewable place, exactly as `humanLabel` does for column keys.
//
// MAINTAINER NOTE: the expansions of the abbreviated rules (PREG_TOX, CALCULI, NEURO,
// SKIN) are this author's reading of standard veterinary shorthand, not text taken from a
// farm document. They are copy and are cheap to correct; nothing but the label changes.
var deathCauseLabels = map[string]string{
	"ACIDOSIS":    "Acidosis",
	"ANEMIA":      "Anemia",
	"ARTHRITIS":   "Arthritis",
	"BLOAT":       "Bloat",
	"BODY_EDEMA":  "Body edema",
	"CALCULI":     "Urinary calculi",
	"DIARRHEA":    "Diarrhea",
	"FEVER":       "Fever",
	"FLOPPY_KID":  "Floppy kid",
	"FLYSTRIKE":   "Flystrike",
	"FOOT_ROT":    "Foot rot",
	"FRACTURE":    "Fracture",
	"HEAT_STRESS": "Heat stress",
	"HYPOTHERMIA": "Hypothermia",
	"JAUNDICE":    "Jaundice",
	"LAMINITIS":   "Laminitis",
	"LUMPS":       "Lumps",
	"MASTITIS":    "Mastitis",
	"METRITIS":    "Metritis",
	"MILK_FEVER":  "Milk fever",
	"NAVEL_ILL":   "Navel ill",
	"NEURO":       "Neurological",
	"ORF":         "Orf",
	"PINKEYE":     "Pinkeye",
	"POX":         "Pox",
	"PPR":         "PPR",
	"PREG_TOX":    "Pregnancy toxaemia",
	"PROLAPSE":    "Prolapse",
	"RED_URINE":   "Red urine",
	"SKIN":        "Skin condition",
	"TETANUS":     "Tetanus",
	"UDDER_EDEMA": "Udder edema",
	"WOUNDS":      "Wounds",
}

// deathCauseLabel returns the farm's word for a rule.
//
// A rule with no mapping falls back to a HUMANISED id rather than the raw key, so a rule
// added to the register tomorrow reaches the dropdown reading "Ring worm" instead of
// "RING_WORM" — and never disappears from it, which is what a fail-closed empty label
// would do. The map is still the right place to give it the farm's own wording.
func deathCauseLabel(ruleID string) string {
	if label, ok := deathCauseLabels[ruleID]; ok {
		return label
	}
	humanised := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ruleID), "_", " "))
	if humanised == "" {
		return ""
	}
	return strings.ToUpper(humanised[:1]) + humanised[1:]
}
