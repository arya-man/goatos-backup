#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo/backend"

export GOATOS_RUN_POSTGRES_TESTS=1
export GOATOS_REQUIRE_DOCKER=1

go test ./internal/vaccination/adapters/postgres -run 'TestRecordCompletionsFromSubmission(MaterializesEveryVaccineObligationForOneScanItem|ClosesNeighborBatchObligationsFromScanAnchor|ClosesStandaloneSameDayRFIDObligations|SkipsTerminalObligation|IgnoresSubmittedItemsWithoutOpenObligations|SkipsAlreadyCompletedNeighborItems)|TestRecordCompletionsFromVaccinationSessionTask' -count=1
go test ./internal/sop/adapters/postgres -run 'Test(CompletedTaskProofRefsKeepsGoatNeighborProofsForVaccinationFanout|ShedCompletionReadinessCountsDualVaccineObligationsAtAnimalGrain|SubmitTask.*(PartitionedVaccinationSubmit|ShedProofFilters|ShedScopedKey))' -count=1
go test ./internal/vaccinationexecution/adapters/postgres -run 'TestClassifyScanTagFindsNeighborPartitionFutureTargetObligations' -count=1
go test ./internal/sopbridge -run 'TestVaccinationSubmissionBridgeFinalizesNeighborSubmitAlertsAndClosesEveryObligation' -count=1
go test ./internal/notificationbridge -run 'TestVerificationEventConsumer_PendingVaccinationGoatsDoNotCollapseAtSubmissionGrain' -count=1
