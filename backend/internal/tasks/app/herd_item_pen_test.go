package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestHerdVerifierItemsCarryTheAnimalsPen: birth and death verifier items carried the shed and
// NO partition, so a kid in Castro 1 was reviewed under "Castro" (the shed-scoped resolver goes
// bare when a shed has several pens) -- while the label comment already promised "the pen is
// carried on the item's own shed_id/partition". The item names the animal's own pen when the
// animal still stands in the workflow's shed (E2E 2026-09-17).
func TestHerdVerifierItemsCarryTheAnimalsPen(t *testing.T) {
	shed := "55555555-5555-4555-8555-555555555555"

	svc, repo, enq, workflowID := newServiceWithMotherWorkflow(t)
	facts := repo.goats[testGoat]
	facts.ShedID, facts.PartitionLabel = &shed, "Part 3"
	repo.goats[testGoat] = facts
	w := repo.workflows[workflowID]
	w.ShedID = &shed
	repo.workflows[workflowID] = w
	recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyBabiesStillInside, "proof-babies", "k1")
	if len(enq.birthCalls) != 1 || enq.birthCalls[0].PartitionLabel != "Part 3" || enq.birthCalls[0].ShedID != shed {
		t.Fatalf("birth step item pen = %+v, want shed %s partition Part 3", enq.birthCalls, shed)
	}

	dsvc, drepo, denq, deathID := newServiceWithDeathWorkflow(t)
	dfacts := drepo.goats[testGoat]
	dfacts.ShedID, dfacts.PartitionLabel = &shed, "Part 3"
	drepo.goats[testGoat] = dfacts
	dw := drepo.workflows[deathID]
	dw.ShedID = &shed
	drepo.workflows[deathID] = dw
	completeDeathStep(t, dsvc, drepo, deathID, domain.ActionKeyDeathVideo, "k-death", domain.ProofItem{Ref: "v-death", Kind: domain.ProofKindVideo})
	completeDeathStep(t, dsvc, drepo, deathID, domain.ActionKeyPostMortemVideo, "k-post", domain.ProofItem{Ref: "v-post", Kind: domain.ProofKindVideo})
	releaseApprovedDeath(t, dsvc, drepo, deathID)
	if len(denq.calls) != 1 || denq.calls[0].PartitionLabel != "Part 3" {
		t.Fatalf("death item pen = %+v, want partition Part 3", denq.calls)
	}

	// An animal that has left the workflow's shed names no partition of it.
	other := "66666666-6666-4666-8666-666666666666"
	facts.ShedID = &other
	repo.goats[testGoat] = facts
	recordMotherStep(t, svc, repo, workflowID, domain.ActionKeyMotherLicking, "proof-licking", "k2")
	if got := enq.birthCalls[len(enq.birthCalls)-1].PartitionLabel; got != "" {
		t.Fatalf("partition of a pen the animal is not in = %q, want blank", got)
	}
}
