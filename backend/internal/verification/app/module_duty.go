package app

import (
	"context"
	"strings"
)

// ModuleDutyReader is the actor's own "which modules do I work in" lookup. It is
// deliberately the SAME method /app/bootstrap already calls
// (workforce/ports.Repository.ListGrantedModuleKeys: department_module_grants UNION the
// caller's duty_type='verify' rows in position_module_duties), matched structurally so
// the workforce repository satisfies it as-is. Verification must not grow a second,
// privately-maintained answer to that question -- the drawer a verifier is shown and the
// queue the API will serve them have to be derived from one source or they drift apart
// silently, which is exactly how a Counts verifier ended up able to open vaccination
// proofs.
type ModuleDutyReader interface {
	ListGrantedModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error)
}

// WithModuleDutyReader wires the duty lookup. It is optional: a Service built without it
// keeps the pre-existing behaviour exactly, so a composition root that has not wired it
// (or a test that does not care) is never silently turned into a denial path.
func (s *Service) WithModuleDutyReader(reader ModuleDutyReader) *Service {
	s.duties = reader
	return s
}

// verificationOnlyModuleKey is the coarse department-level grant that names no feature.
// It carries no module identity, so it can neither authorize nor deny a specific module.
const verificationOnlyModuleKey = "verification"

// AuthorizeQueueModule decides whether this actor may read the queue slice they asked for.
//
// THE DEFECT IT CLOSES: the queue endpoints took `category` verbatim from the client and
// gated only on permissions.VerificationReview, which answers "is this person a verifier
// somewhere" and nothing more. Park scope was enforced (verificationParkScope), so the
// leak was not cross-park -- it was cross-MODULE inside parks the caller was already
// entitled to: a Counts verifier could pass category=vaccination_proof and read
// vaccination proof video, work belonging to a module they have no duty in.
//
// FAIL OPEN, NOT CLOSED. This handler is shared by every module, so a rule that guesses
// wrong does not merely under-protect, it locks working verifiers out of their own park.
// An earlier attempt on this branch did exactly that. Therefore this returns nil -- i.e.
// today's behaviour -- in every case where duty is not POSITIVELY known to exclude the
// request:
//
//   - no duty reader wired,
//   - no category/module named (the un-narrowed queue is unchanged; see the note below),
//   - the category is not in the registry, so it selects no module anyway,
//   - the duty lookup errors,
//   - the actor has no duty rows at all (position_module_duties is unseeded for them),
//   - the actor's rows name no feature (a bare department-level "verification" grant).
//
// Only a caller whose duty set is feature-bearing AND does not cover the requested module
// is refused. Note the deliberate limit: an un-narrowed request (no category, no module)
// is NOT filtered down to the actor's modules. Doing so would silently shrink the list a
// counts or feed verifier sees today, which is a behaviour change the maintainer excluded
// during stabilisation. That residual is a reporting item, not something to close here.
func (s *Service) AuthorizeQueueModule(ctx context.Context, tenantID, actorID, category, module string) error {
	if s.duties == nil {
		return nil
	}
	requested := s.requestedModule(category, module)
	if requested == "" {
		return nil
	}
	granted, err := s.duties.ListGrantedModuleKeys(ctx, tenantID, actorID)
	if err != nil {
		return nil
	}
	known := featureModuleKeys(granted)
	if len(known) == 0 {
		return nil
	}
	for _, key := range known {
		if moduleKeysMatch(key, requested) {
			return nil
		}
	}
	return Forbidden("module_not_verified", "you do not verify this module")
}

// requestedModule resolves what the caller actually asked for into the registry's module
// vocabulary. The category is preferred because the category IS the queue scope: the
// registry (populated at composition time by each producing module) already owns
// category -> module, so this reads that mapping instead of restating it.
func (s *Service) requestedModule(category, module string) string {
	if category = strings.TrimSpace(category); category != "" {
		def, ok := s.registry.Get(category)
		if !ok {
			return ""
		}
		return normalizeModuleKey(def.Module)
	}
	return normalizeModuleKey(module)
}

// NOTE ON WHAT THE LIST CONTAINS. ListGrantedModuleKeys is a UNION of the caller's verify
// duties and their department's module grants, so a department member's set is wider than
// their duties alone. That is deliberate: it is the same set /app/bootstrap uses to decide
// which verify entries the person is shown, and a queue that served modules the drawer
// never offered would be the same divergence this fix exists to remove. The practical
// consequence to watch is that a department whose grants omit a module (weighing is in no
// department grant today) makes that module refusable for its members -- correct as
// cross-module refusal, but it is the ONE place this change can produce a 403 that did not
// exist before.

// featureModuleKeys drops the keys that name no feature and normalizes the rest. Mirrors
// the workforce bootstrap's treatment of the same list: a bare "verification" key is a
// department-level grant with no module attached, and a principal holding only that is
// scoped to everything shipped rather than to nothing.
func featureModuleKeys(granted []string) []string {
	out := make([]string, 0, len(granted))
	for _, key := range granted {
		normalized := normalizeModuleKey(key)
		if normalized == "" || normalized == verificationOnlyModuleKey {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// normalizeModuleKey folds the namespaced duty vocabulary ("pc.vaccination",
// "feed.direction") onto the flat module ids the rest of the system speaks. Same
// transformation the workforce bootstrap applies before comparing these keys.
func normalizeModuleKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.TrimPrefix(key, "pc.")
	return strings.ReplaceAll(key, ".", "_")
}

// moduleKeysMatch compares a duty key with a registry module key. They are two spellings
// of one thing that have drifted at the edges -- the duty catalog records feed as
// "feed.direction" while the feed module registers its categories under module "feed" --
// so a segment-prefix match treats those as the same module. The looseness only ever
// grants, never denies, which is the correct direction for a rule that must not invent
// new 403s.
func moduleKeysMatch(duty, requested string) bool {
	if duty == "" || requested == "" {
		return false
	}
	if duty == requested {
		return true
	}
	return strings.HasPrefix(duty, requested+"_") || strings.HasPrefix(requested, duty+"_")
}
