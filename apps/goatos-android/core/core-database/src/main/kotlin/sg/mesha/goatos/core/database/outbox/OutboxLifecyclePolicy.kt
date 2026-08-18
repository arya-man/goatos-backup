package sg.mesha.goatos.core.database.outbox

enum class OutboxUserImpact { USER_VISIBLE_MUTATION, BACKGROUND_SUPPORT_WRITE }

enum class OutboxImmediateUiPolicy {
    DURABLE_LOCAL_MODEL,
    OUTBOX_DERIVED_OVERLAY,
    EXACT_ITEM_STATUS,
    PARENT_OPERATION_STATUS,
    NO_USER_VISIBLE_STATE,
}

enum class OutboxSuccessPolicy {
    DIRECT_LOCAL_RECONCILIATION,
    POST_SUCCESS_REFRESH,
    EXACT_ITEM_REFRESH,
    RESULT_CONSUMED_BY_DEPENDENT,
    NO_USER_VISIBLE_STATE,
}

enum class OutboxTerminalFailurePolicy {
    EXACT_ITEM_ERROR,
    RELOAD_SERVER_TRUTH,
    OUTBOX_OVERLAY_RETRACTION,
    PARENT_OPERATION_BLOCKED,
    NO_USER_VISIBLE_STATE,
}

enum class OutboxProcessDeathPolicy { ROOM_AND_OUTBOX, OUTBOX_REPLAY, DEPENDENT_OUTBOX_REPLAY }

data class OutboxLifecyclePolicy(
    val userImpact: OutboxUserImpact,
    val immediate: OutboxImmediateUiPolicy,
    val success: OutboxSuccessPolicy,
    val terminalFailure: OutboxTerminalFailurePolicy,
    val processDeath: OutboxProcessDeathPolicy,
)

/**
 * Required lifecycle declaration for every operation accepted by the Android outbox.
 *
 * This is a production-owned design contract, not a substitute for behavior tests. The exhaustive
 * [when] makes a new [OutboxOpType] fail Kotlin compilation until its four lifecycle decisions are
 * reviewed. A structural CI guard independently verifies this source shape without a Gradle build.
 */
val OutboxOpType.lifecyclePolicy: OutboxLifecyclePolicy
    get() = when (this) {
        OutboxOpType.SHED_SUBMIT -> lifecycle(
            userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
            immediate = OutboxImmediateUiPolicy.EXACT_ITEM_STATUS,
            success = OutboxSuccessPolicy.EXACT_ITEM_REFRESH,
            terminalFailure = OutboxTerminalFailurePolicy.EXACT_ITEM_ERROR,
            processDeath = OutboxProcessDeathPolicy.OUTBOX_REPLAY,
        )
        OutboxOpType.SCAN_CAPTURE -> lifecycle(
            userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
            immediate = OutboxImmediateUiPolicy.DURABLE_LOCAL_MODEL,
            success = OutboxSuccessPolicy.DIRECT_LOCAL_RECONCILIATION,
            terminalFailure = OutboxTerminalFailurePolicy.EXACT_ITEM_ERROR,
            processDeath = OutboxProcessDeathPolicy.ROOM_AND_OUTBOX,
        )
        OutboxOpType.SCAN_ATTEMPT -> lifecycle(
            userImpact = OutboxUserImpact.BACKGROUND_SUPPORT_WRITE,
            immediate = OutboxImmediateUiPolicy.NO_USER_VISIBLE_STATE,
            success = OutboxSuccessPolicy.NO_USER_VISIBLE_STATE,
            terminalFailure = OutboxTerminalFailurePolicy.NO_USER_VISIBLE_STATE,
            processDeath = OutboxProcessDeathPolicy.OUTBOX_REPLAY,
        )
        OutboxOpType.PROOF_UPLOAD -> lifecycle(
            userImpact = OutboxUserImpact.BACKGROUND_SUPPORT_WRITE,
            immediate = OutboxImmediateUiPolicy.PARENT_OPERATION_STATUS,
            success = OutboxSuccessPolicy.RESULT_CONSUMED_BY_DEPENDENT,
            terminalFailure = OutboxTerminalFailurePolicy.PARENT_OPERATION_BLOCKED,
            processDeath = OutboxProcessDeathPolicy.DEPENDENT_OUTBOX_REPLAY,
        )
        OutboxOpType.RESCHEDULE -> exactItemLifecycle()
        OutboxOpType.VERIFY_TASK -> exactItemLifecycle()
        OutboxOpType.REWORK_TASK -> exactItemLifecycle()
        OutboxOpType.VERIFICATION_VERDICT -> exactItemLifecycle()
        OutboxOpType.VERIFICATION_CLOSE -> exactItemLifecycle()
        OutboxOpType.VERIFICATION_CLOSE_SUBMISSION -> exactItemLifecycle()
        OutboxOpType.VERIFICATION_CLOSE_BATCH -> exactItemLifecycle()
        OutboxOpType.WEIGHING_WEIGHT_CORRECTION -> exactItemLifecycle()
        OutboxOpType.COUNTS_SHIFTING -> exactItemPostSuccessRefreshLifecycle()
        OutboxOpType.COUNTS_BIRTH -> exactItemPostSuccessRefreshLifecycle()
        OutboxOpType.COUNTS_DEATH -> exactItemPostSuccessRefreshLifecycle()
        OutboxOpType.COUNTS_APPROVAL_APPROVE -> optimisticRefreshLifecycle()
        OutboxOpType.COUNTS_APPROVAL_REJECT -> optimisticRefreshLifecycle()
        OutboxOpType.SHIFTING_COMPLETE -> exactItemPostSuccessRefreshLifecycle()
        OutboxOpType.SHIFTING_CANCEL -> exactItemPostSuccessRefreshLifecycle()
        OutboxOpType.COUNTS_PROMOTE_IDENTIFIER -> optimisticRefreshLifecycle()
        OutboxOpType.FEED_DIRECTION_COMPLETE -> overlayRefreshLifecycle()
        OutboxOpType.FEED_DISTRIBUTION_COMPLETE -> overlayDirectReconcileLifecycle()
        OutboxOpType.FEED_PACKING_COMPLETE -> overlayDirectReconcileLifecycle()
        OutboxOpType.MILK_PREPARATION_SUBMIT -> overlayRefreshLifecycle()
        OutboxOpType.MILK_FEEDING_SUBMIT -> overlayRefreshLifecycle()
        OutboxOpType.FEED_TRANSPORT_SUBMIT -> overlayDirectReconcileLifecycle()
        OutboxOpType.WORKFLOW_ACTION_ANSWER -> optimisticRefreshLifecycle()
        OutboxOpType.WORKFLOW_ACTION_COMPLETE -> optimisticRefreshLifecycle()
        OutboxOpType.HEALTH_CASE_OPEN -> overlayRefreshLifecycle()
        OutboxOpType.HEALTH_TREATMENT_COMPLETE -> optimisticRefreshLifecycle()
        OutboxOpType.WEIGHING_ANIMAL_OBSERVATION -> durableDirectReconcileLifecycle()
        OutboxOpType.WEIGHING_SHED_OBSERVATION -> durableDirectReconcileLifecycle()
        OutboxOpType.WEIGHING_SCOPE_SUBMIT -> durableDirectReconcileLifecycle()
    }

private fun exactItemLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.EXACT_ITEM_STATUS,
    success = OutboxSuccessPolicy.EXACT_ITEM_REFRESH,
    terminalFailure = OutboxTerminalFailurePolicy.EXACT_ITEM_ERROR,
    processDeath = OutboxProcessDeathPolicy.OUTBOX_REPLAY,
)

private fun exactItemPostSuccessRefreshLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.EXACT_ITEM_STATUS,
    success = OutboxSuccessPolicy.POST_SUCCESS_REFRESH,
    terminalFailure = OutboxTerminalFailurePolicy.EXACT_ITEM_ERROR,
    processDeath = OutboxProcessDeathPolicy.OUTBOX_REPLAY,
)

private fun optimisticRefreshLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.DURABLE_LOCAL_MODEL,
    success = OutboxSuccessPolicy.POST_SUCCESS_REFRESH,
    terminalFailure = OutboxTerminalFailurePolicy.RELOAD_SERVER_TRUTH,
    processDeath = OutboxProcessDeathPolicy.ROOM_AND_OUTBOX,
)

private fun overlayRefreshLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.OUTBOX_DERIVED_OVERLAY,
    success = OutboxSuccessPolicy.POST_SUCCESS_REFRESH,
    terminalFailure = OutboxTerminalFailurePolicy.OUTBOX_OVERLAY_RETRACTION,
    processDeath = OutboxProcessDeathPolicy.OUTBOX_REPLAY,
)

private fun overlayDirectReconcileLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.OUTBOX_DERIVED_OVERLAY,
    success = OutboxSuccessPolicy.DIRECT_LOCAL_RECONCILIATION,
    terminalFailure = OutboxTerminalFailurePolicy.OUTBOX_OVERLAY_RETRACTION,
    processDeath = OutboxProcessDeathPolicy.ROOM_AND_OUTBOX,
)

private fun durableDirectReconcileLifecycle() = lifecycle(
    userImpact = OutboxUserImpact.USER_VISIBLE_MUTATION,
    immediate = OutboxImmediateUiPolicy.DURABLE_LOCAL_MODEL,
    success = OutboxSuccessPolicy.DIRECT_LOCAL_RECONCILIATION,
    terminalFailure = OutboxTerminalFailurePolicy.EXACT_ITEM_ERROR,
    processDeath = OutboxProcessDeathPolicy.ROOM_AND_OUTBOX,
)

private fun lifecycle(
    userImpact: OutboxUserImpact,
    immediate: OutboxImmediateUiPolicy,
    success: OutboxSuccessPolicy,
    terminalFailure: OutboxTerminalFailurePolicy,
    processDeath: OutboxProcessDeathPolicy,
) = OutboxLifecyclePolicy(userImpact, immediate, success, terminalFailure, processDeath)
