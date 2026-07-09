package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Shared process-integrity value objects reused by Control Tower alerts and
 * Protocol Adherence rows (schemas ProcessIntegrityOwner / ProcessIntegrityEvidence).
 * All fields are optional in the contract, so defaults keep deserialization lenient.
 */
@Serializable
data class ProcessIntegrityOwnerDto(
    @SerialName("operator_id") val operatorId: String? = null,
    @SerialName("operator_name") val operatorName: String? = null,
    @SerialName("park_head_id") val parkHeadId: String? = null,
    @SerialName("park_head_name") val parkHeadName: String? = null,
    @SerialName("verifier_id") val verifierId: String? = null,
    @SerialName("verifier_name") val verifierName: String? = null,
    @SerialName("escalation_owner_id") val escalationOwnerId: String? = null,
    @SerialName("escalation_owner_name") val escalationOwnerName: String? = null,
)

@Serializable
data class ProcessIntegrityEvidenceDto(
    @SerialName("proof_ids") val proofIds: List<String> = emptyList(),
    @SerialName("evidence_count") val evidenceCount: Int = 0,
    @SerialName("latest_evidence_at") val latestEvidenceAt: String? = null,
    @SerialName("latest_rejection_reason") val latestRejectionReason: String? = null,
    @SerialName("audit_ref") val auditRef: String? = null,
)
