package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.database.outbox.OutboxEntity

internal fun OutboxEntity.referencedProofOutboxItemId(): String =
    payloadJson.referencedProofOutboxItemId()

internal fun String.referencedProofOutboxItemId(): String =
    PROOF_OUTBOX_ITEM_ID_REGEX.find(this)?.groupValues?.getOrNull(1)
        ?: PROOF_OUTBOX_ITEM_IDS_VALUE_REGEX.find(this)?.groupValues?.getOrNull(1)
        ?: ""

private val PROOF_OUTBOX_ITEM_ID_REGEX = Regex(
    """"[^"]*(?:proof[_a-z]*_outbox_item_id|photo[_a-z]*_outbox_item_id|proofOutboxItemId|ProofOutboxItemId)"\s*:\s*"([^"]+)"""",
)

private val PROOF_OUTBOX_ITEM_IDS_VALUE_REGEX = Regex(
    """"proof_outbox_item_ids"\s*:\s*\{[^}]*?"[^"]+"\s*:\s*"([^"]+)"""",
)
