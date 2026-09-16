package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.first
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository

/**
 * Retires this screen's PREVIOUS stage submit when it was rejected for good and the operator is
 * submitting a corrected one under a NEW key.
 *
 * The outbox is a per-group FIFO and a TERMINALLY failed row holds its lane (OutboxDao's
 * eligibleForDrain: a dead-lettered or conflicted older row blocks everything queued behind it,
 * so a shed submit can never overtake a scan that never reached the server). That rule is right
 * for a scan, and wrong for THIS case: the server refused the submit itself (`422` -- an answer
 * out of range, a slot the card no longer has), the operator fixed the card on screen and pressed
 * Submit again, and the corrected write carries a different idempotency key (the key is the proof
 * set plus the answers). Behind the dead letter it never drained -- seen on the phone 2026-09-16:
 * "How many bags?" answered 500 (max 200) -> 422 -> corrected to 12 -> the screen said
 * "Submitted" and nothing ever reached the server.
 *
 * A same-key resubmit needs none of this (`insertOrExistingRow` re-arms the row). Only a row that
 * is FAILED and out of retries or conflicted is retired; a queued, in-flight or still-retrying
 * submit is left alone -- the new one waits behind it, in order, as it should.
 */
internal suspend fun SyncRepository.retireRejectedSubmit(previousSubmitItemId: String?, newIdempotencyKey: String) {
    val id = previousSubmitItemId?.takeIf { it.isNotBlank() } ?: return
    val item = observeItem(id).first() ?: return
    if (item.idempotencyKey == newIdempotencyKey) return
    if (item.status != SyncItemStatus.FAILED || !(item.conflict || item.isDeadLetter)) return
    deleteOutboxItem(id)
}
