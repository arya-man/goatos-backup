package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
// PLUS HEALTH CONFIG (maintainer decision 2026-09-25): the list is the built-in register
// diseases PLUS every disease the tenant has authored in Health Config -- the problem rules
// of its PUBLISHED diagnosis registers (the same documents the engine diagnoses from) and
// every ACTIVE disease on the treatment tab, folded into one row per disease
// (domain.WithAuthoredDiseases). A disease later retired stays NAMED on the deaths already
// filed under it; only a new death is limited to what is active today.
//
// The BUILT-IN half is built ONCE: the registers are embedded YAML validated at process start
// and cannot change under a running process. The TENANT half is read per tenant and held for
// a short, bounded time (see tenantCatalogTTL), dropped at once when this process publishes a
// Health Config change (Invalidate).
type DeathCauseCatalogService struct {
	once    sync.Once
	builtIn builtInDeathCauses
	err     error

	source DeathCauseSource
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]tenantDeathCauses
}

// DeathCauseSource reads what a tenant's Health Config contributes to the death form.
type DeathCauseSource interface {
	DeathCauseSources(ctx context.Context, tenantID string) (domain.DeathCauseTenantSources, error)
}

const (
	// tenantCatalogTTL bounds how stale another API instance's list can be after a publish
	// it did not see. Health Config changes a few times a month; a death form opened half a
	// minute after a publish missing the new disease is the whole cost, and the local
	// publish path invalidates immediately.
	tenantCatalogTTL = 30 * time.Second
	// tenantCatalogMaxEntries caps the cache so it cannot grow with the number of tenants.
	tenantCatalogMaxEntries = 256
)

type builtInDeathCauses struct {
	catalog domain.DeathCauseCatalog
	rules   []domain.RegisterRule
}

type tenantDeathCauses struct {
	catalog domain.DeathCauseCatalog
	// labels names every cause key this tenant has ever been able to record, active or
	// not, so a death filed under a disease since retired still reads as that disease.
	labels  map[string]string
	expires time.Time
}

func NewDeathCauseCatalogService() *DeathCauseCatalogService {
	return &DeathCauseCatalogService{now: time.Now, cache: map[string]tenantDeathCauses{}}
}

// WithTenantSource adds the tenant's Health Config diseases to the built-in list. Without it
// the service serves the built-in register alone, exactly as before 2026-09-25.
func (s *DeathCauseCatalogService) WithTenantSource(source DeathCauseSource) *DeathCauseCatalogService {
	s.source = source
	return s
}

// Invalidate drops one tenant's cached list. Health Config's publish paths call it, so the
// death form on this instance shows a newly published disease on its next read.
func (s *DeathCauseCatalogService) Invalidate(tenantID string) {
	s.mu.Lock()
	delete(s.cache, strings.TrimSpace(tenantID))
	s.mu.Unlock()
}

// Catalog returns the whole searchable vocabulary for one tenant.
func (s *DeathCauseCatalogService) Catalog(ctx context.Context, tenantID string) (domain.DeathCauseCatalog, error) {
	entry, err := s.tenant(ctx, tenantID)
	if err != nil {
		return domain.DeathCauseCatalog{}, err
	}
	return entry.catalog, nil
}

// ValidateCause refuses a cause the tenant's vocabulary does not name.
//
// A cause that cannot be resolved is REJECTED rather than stored as typed: the whole value
// of a coded cause is that it groups, and one death filed under 'MASTITIS' beside another
// under a near-miss is two diseases on the board and one in the barn. An operator who
// cannot find the disease records a NORMAL death and says so in the note.
// The signature is STRING-SHAPED rather than domain-typed so the module that records
// deaths can hold a narrow port over it without importing health's types. Counts is that
// caller: it records that an animal died, and has no business knowing what diseases exist.
//
// It is called ONLY when a death is RAISED. A cause already stored is never re-validated,
// which is what keeps a death filed under a since-retired disease valid.
func (s *DeathCauseCatalogService) ValidateCause(ctx context.Context, tenantID, key, kind string) error {
	catalog, err := s.Catalog(ctx, tenantID)
	if err != nil {
		return err
	}
	return domain.ValidateDeathCause(domain.DeathCause{Key: key, Kind: kind}, catalog)
}

// LabelDeathCause resolves a stored cause key to the farm's word for the disease, for the
// Counts -> Mortality read. A register rule or Health Config disease is looked up by key --
// including a disease that is no longer active, so history keeps its name. An unknown key is
// returned humanised rather than blank, so a cause recorded under a rule the register has
// since retired still shows on the mortality board instead of vanishing into "no cause".
func (s *DeathCauseCatalogService) LabelDeathCause(ctx context.Context, tenantID, key string) string {
	if entry, err := s.tenant(ctx, tenantID); err == nil {
		if label, ok := entry.labels[key]; ok {
			return label
		}
	}
	return domain.DeathCauseLabel(key)
}

func (s *DeathCauseCatalogService) tenant(ctx context.Context, tenantID string) (tenantDeathCauses, error) {
	s.once.Do(s.build)
	if s.err != nil {
		return tenantDeathCauses{}, s.err
	}
	tenantID = strings.TrimSpace(tenantID)
	if s.source == nil || tenantID == "" {
		return composeTenantDeathCauses(s.builtIn, domain.DeathCauseTenantSources{}), nil
	}

	now := s.now()
	s.mu.Lock()
	if entry, ok := s.cache[tenantID]; ok && now.Before(entry.expires) {
		s.mu.Unlock()
		return entry, nil
	}
	s.mu.Unlock()

	sources, err := s.source.DeathCauseSources(ctx, tenantID)
	if err != nil {
		// Failing is honest: serving the built-in half alone would silently drop the farm's
		// own diseases from the dropdown with nothing on screen to say so.
		return tenantDeathCauses{}, fmt.Errorf("health: read tenant death causes: %w", err)
	}
	entry := composeTenantDeathCauses(s.builtIn, sources)
	entry.expires = now.Add(tenantCatalogTTL)

	s.mu.Lock()
	if len(s.cache) >= tenantCatalogMaxEntries {
		for id, cached := range s.cache {
			if !now.Before(cached.expires) {
				delete(s.cache, id)
			}
		}
		if len(s.cache) >= tenantCatalogMaxEntries {
			s.cache = map[string]tenantDeathCauses{}
		}
	}
	s.cache[tenantID] = entry
	s.mu.Unlock()
	return entry, nil
}

// composeTenantDeathCauses is the whole merge, pure so it is testable without a database.
func composeTenantDeathCauses(builtIn builtInDeathCauses, sources domain.DeathCauseTenantSources) tenantDeathCauses {
	rules := make([]domain.RegisterRule, 0, len(builtIn.rules)+len(sources.RegisterRules))
	rules = append(rules, builtIn.rules...)
	for _, rule := range sources.RegisterRules {
		if strings.TrimSpace(rule.Label) == "" {
			rule.Label = domain.DeathCauseLabel(rule.ID)
		}
		rules = append(rules, rule)
	}
	versions := append(append([]string(nil), builtIn.catalog.RegisterVersions...), sources.RegisterVersions...)
	versions = dedupeStrings(versions)

	catalog := domain.WithAuthoredDiseases(domain.BuildDeathCauseCatalog(rules, versions), rules, sources.Diseases)

	labels := make(map[string]string, len(catalog.Options)+len(sources.Diseases))
	for _, disease := range sources.Diseases {
		if key, label := strings.TrimSpace(disease.Key), strings.TrimSpace(disease.Label); key != "" && label != "" {
			labels[key] = label
		}
	}
	for _, option := range catalog.Options {
		labels[option.Key] = option.Label
	}
	return tenantDeathCauses{catalog: catalog, labels: labels}
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok || strings.TrimSpace(v) == "" {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
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
				// The embedded register names its course through sop_ref, resolved the
				// same way the seeded authored register's `treats` was written.
				Treats: domain.SOPRefToDiseaseKey(rule.SOPRef),
			})
		}
	}
	s.builtIn = builtInDeathCauses{catalog: domain.BuildDeathCauseCatalog(rules, versions), rules: rules}
}
