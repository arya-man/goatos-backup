# Analytics Event Coverage Matrix

This matrix documents proof-flow analytics coverage across all features, identifying event emissions and known gaps. Columns represent event lifecycle stages; rows represent workflows. Cell values are event names or "GAP" where tracking is absent.

## Coverage Matrix

| Workflow | Screen Open | Capture Start | Capture Success | Capture Fail | Proof Upload Queued | Proof Upload Synced | Proof Upload Failed | Submit Queued | Submit Success | Submit Fail | Retry/Dead-Letter | Teammate Hydration | Read-Only Blocked |
|----------|-------------|----------------|-----------------|--------------|---------------------|---------------------|---------------------|---------------|---------------|-----------|--------------------|-------------------|------------------|
| **Feed Distribution** | FEED_DISTRIBUTION_OPENED | FEED_DISTRIBUTION_CAPTURE_TAPPED | FEED_DISTRIBUTION_PROOF_CAPTURED | GAP | GAP | FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED | FEED_DISTRIBUTION_FAILURE | GAP | FEED_DISTRIBUTION_SUBMITTED | FEED_DISTRIBUTION_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ | FEED_DISTRIBUTION_REOPEN_BLOCKED |
| **Feed Packing** | FEED_PACKING_COMPLETE_OPENED | FEED_PACKING_CAPTURE_TAPPED | FEED_PACKING_VIDEO_CAPTURED | GAP | GAP | FEED_PACKING_PROOF_UPLOAD_SYNCED | FEED_PACKING_COMPLETE_FAILURE | GAP | FEED_PACKING_SUBMITTED | FEED_PACKING_COMPLETE_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | GAP | GAP |
| **Feed Transport** | FEED_TRANSPORT_OPENED | FEED_TRANSPORT_CAPTURE_TAPPED | FEED_TRANSPORT_VIDEO_CAPTURED | GAP | GAP | FEED_TRANSPORT_PROOF_UPLOAD_SYNCED | FEED_TRANSPORT_FAILURE | GAP | FEED_TRANSPORT_SUBMITTED | FEED_TRANSPORT_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | GAP | GAP |
| **Vaccination Scan** | VACCINATION_SHEDS_VIEWED | GAP | VACCINATION_SCAN | VACCINATION_SCAN_REJECTED | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP | VACCINATION_OPEN_BLOCKED |
| **Vaccination Finalize** | VACCINATION_SHEDS_VIEWED | VACCINATION_PROOF_CAPTURE_ATTEMPT | VACCINATION_PROOF_CAPTURE_SUCCESS | VACCINATION_PROOF_CAPTURE_FAILURE | GAP | GAP | WEIGHING_PROOF_UPLOAD_FAILED | GAP | VACCINATION_SUBMIT_SUCCESS | VACCINATION_SUBMIT_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | GAP | VACCINATION_OPEN_BLOCKED |
| **Weighing Individual** | WEIGHING_VIEWED | WEIGHING_PROOF_CAPTURE_ATTEMPT | WEIGHING_PROOF_CAPTURE_SUCCESS | WEIGHING_PROOF_CAPTURE_FAILURE | GAP | GAP | WEIGHING_PROOF_UPLOAD_FAILED | GAP | WEIGHING_SUBMIT_SUCCESS | WEIGHING_SUBMIT_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | GAP | GAP |
| **Weighing Lumpsum** | WEIGHING_VIEWED | WEIGHING_WEIGHT_CAPTURE_ATTEMPT | WEIGHING_WEIGHT_CAPTURE_SUCCESS | WEIGHING_WEIGHT_CAPTURE_FAILURE | GAP | GAP | GAP | GAP | WEIGHING_SUBMIT_SUCCESS | WEIGHING_SUBMIT_FAILURE | PROOF_VIDEO_UPLOAD_RETRY, PROOF_VIDEO_DEAD_LETTER | GAP | GAP |
| **Milk Preparation** | VACCINATION_SHEDS_VIEWED* | WEIGHING_PROOF_CAPTURE_ATTEMPT* | WEIGHING_PROOF_CAPTURE_SUCCESS* | WEIGHING_PROOF_CAPTURE_FAILURE* | GAP | GAP | GAP | WEIGHING_SUBMIT_ATTEMPT* | WEIGHING_SUBMIT_SUCCESS* | WEIGHING_SUBMIT_FAILURE* | GAP | GAP | GAP |
| **Milk Feeding** | VACCINATION_SHEDS_VIEWED* | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP | GAP |
| **Shifting** | COUNTS_SHIFTING_PENDING_VIEWED | GAP | COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED | GAP | GAP | GAP | GAP | GAP | COUNTS_SHIFTING_EXECUTE_COMPLETED | COUNTS_WRITE_FAILURE | SYNC_WRITE_DEAD | GAP | GAP |
| **Birth** | WORKFLOW_LIST_VIEWED | WORKFLOW_VIDEO_CAPTURED | WORKFLOW_VIDEO_CAPTURED | GAP | GAP | GAP | GAP | GAP | COUNTS_BIRTH_SUBMITTED | COUNTS_WRITE_FAILURE | SYNC_WRITE_DEAD | GAP | GAP |
| **Death** | WORKFLOW_LIST_VIEWED | WORKFLOW_VIDEO_CAPTURED | WORKFLOW_VIDEO_CAPTURED | GAP | GAP | GAP | GAP | GAP | COUNTS_DEATH_SUBMITTED | COUNTS_WRITE_FAILURE | SYNC_WRITE_DEAD | GAP | GAP |
| **Health** | HEALTH_VIEWED | WORKFLOW_VIDEO_CAPTURED | WORKFLOW_VIDEO_CAPTURED | GAP | GAP | GAP | GAP | GAP | HEALTH_CASE_SUBMITTED | HEALTH_WRITE_FAILURE | SYNC_WRITE_DEAD | GAP | GAP |
| **Generic Workflow** | WORKFLOW_LIST_VIEWED | WORKFLOW_VIDEO_CAPTURED | WORKFLOW_VIDEO_CAPTURED | GAP | GAP | GAP | GAP | GAP | WORKFLOW_ACTION_COMPLETED | SYNC_WRITE_DEAD | SYNC_WRITE_DEAD | GAP | GAP |

*Milk events use WEIGHING_* event names with kind="milk_*" prefix for now; dedicated milk_* event names may be added in future iterations.

## Summary of Coverage

### Fully Covered (All Lifecycle Stages)
- Feed Distribution: 13/14 stages (missing: Capture Fail as distinct event)
- Vaccination (Finalize): 10/14 stages
- Weighing (Individual): 10/14 stages

### Partially Covered
- Feed Packing: 6/14 stages
- Feed Transport: 6/14 stages
- Vaccination (Scan): 4/14 stages
- Weighing (Lumpsum): 7/14 stages
- Shifting: 5/14 stages
- Birth/Death/Health: 5/14 stages each

### Minimal/No Coverage
- Milk Preparation: 8/14 stages (recently added in P2 durability fix)
- Milk Feeding: 1/14 stages (screen open only)

## Key Gaps and Notes

### Critical Gaps (Impact: High)
1. **Proof Capture Failure as distinct event**: Most workflows emit PROOF_CAPTURE_SUCCESS but do not emit a separate PROOF_CAPTURE_FAILURE when the operator cancels or the device fails to record. Only distinguishable by absence of success event.
   - Affected: All proof-capture workflows
   - Fix: Emit PROOF_CAPTURE_FAILURE on cancellation and device errors
   - Effort: 1-2 features per sprint

2. **Proof Upload Queue Signal**: Proof capture writes proofs to the outbox, but no event marks "proof upload queued" until success/failure. A queued-but-stuck proof is invisible for minutes until retry/dead-letter fires.
   - Affected: All proof workflows
   - Fix: Emit PROOF_UPLOAD_QUEUED immediately after outbox enqueue
   - Effort: 1 PR, 3-4 call sites

3. **Milk Feeding Proof Capture**: Milk feeding has NO proof-capture analytics beyond screen open. The two mandatory proofs (clean bottles, mixing) are captured with no funnel visibility.
   - Affected: Milk Feeding only
   - Fix: Inject analytics into milk feeding capture; emit WEIGHING_PROOF_CAPTURE_* events with kind="milk_feeding_*"
   - Effort: 1 PR, 2-3 capture call sites

### Medium Gaps (Impact: Medium)
1. **Packing/Transport Teammate Hydration**: Feed distribution tracks teammate captures read/adopted; packing/transport do not.
   - Affected: Feed Packing, Feed Transport
   - Fix: Wire same team-capture read path used in distribution
   - Effort: 1 PR, 2 call sites

2. **Shifting Video Capture Events**: Shifting emits only EXECUTE_VIDEO_CAPTURED; no CAPTURE_ATTEMPT or FAILURE events.
   - Affected: Shifting only
   - Fix: Emit CAPTURE_ATTEMPT before capture opens; CAPTURE_FAILURE on cancel/error
   - Effort: 1 PR, 1 call site

3. **Birth/Death/Health Proof Capture**: These workflows emit WORKFLOW_VIDEO_CAPTURED but not capture start/fail. Operator taps "Record video" and it opens the camera with zero telemetry until the clip either completes or the operator cancels (no event).
   - Affected: Birth, Death, Health
   - Fix: Emit WORKFLOW_CAPTURE_ATTEMPT and WORKFLOW_CAPTURE_FAILURE
   - Effort: 1 PR, 3 call sites

### Low Gaps (Impact: Low)
1. **Retry/Dead-Letter Granularity**: SYNC_WRITE_DEAD is generic; no event distinguishes "ran out of retries" from "server rejected". Funnels can't segment failure causes.
   - Fix: Add REASON param with "attempts_exhausted" vs "conflict"
   - Effort: Already implemented in WEIGHING_PROOF_UPLOAD_FAILED; standardize across other ops
   - Effort: 1 PR, audit all dead-letter call sites

2. **Read-Only Block Reasons**: FEED_DISTRIBUTION_REOPEN_BLOCKED and VACCINATION_OPEN_BLOCKED lack reason parameters. Operator sees "can't do this" with no funnel context for why.
   - Fix: Add REASON param with gates like "pending_verification", "permission_denied", "scheduled_later"
   - Effort: 1 PR, 2-3 call sites

## Durability Status

**Critical events now durable** (P2 fix 2026-08-15):
- proof_processing_failed
- SYNC_WRITE_DEAD
- FEED_DISTRIBUTION_LIVE_STATUS_CHANGED
- FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ
- WEIGHING_CAPTURE_FAILURE
- WEIGHING_WEIGHT_CAPTURE_FAILURE
- WEIGHING_PROOF_CAPTURE_FAILURE

**Not yet durable** (best-effort, process-death loss possible):
- All other events in this matrix

**Candidates for durability upgrade**:
- VACCINATION_PROOF_CAPTURE_FAILURE (high-value workflow, not yet durable)
- PROOF_VIDEO_UPLOAD_RETRY (signals retry loop; valuable for retry-attack debugging)
- PROOF_VIDEO_DEAD_LETTER (terminal state; high-value for stuck-work debugging)

## Recommendations

### Short Term (This Sprint)
1. Add capture-fail events to milk feeding (1 PR)
2. Standardize REASON param on retry/dead-letter across all ops (1 PR)

### Medium Term (Next 2 Sprints)
1. Wire proof-upload-queued signal (1-2 PRs)
2. Add capture start/fail events to birth/death/health (1 PR)
3. Extend milk eating capture analytics (1 PR, if scope allows)

### Long Term (Backlog)
1. Decouple event names from weighing/vaccination (milk_* specific names)
2. Extend read-only block reasons across all workflows
3. Evaluate teammate-hydration extension to packing/transport
