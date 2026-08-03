package sg.mesha.goatos.core.data

import androidx.room.Database
import androidx.room.RoomDatabase
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.RfidScanAttemptDao
import sg.mesha.goatos.core.database.capture.RfidScanAttemptEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.data.cache.AdherenceCacheDao
import sg.mesha.goatos.core.data.cache.AdherenceCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleDao
import sg.mesha.goatos.core.data.cache.CalendarScheduleEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleRemoteKeyDao
import sg.mesha.goatos.core.data.cache.CalendarScheduleRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.ControlTowerCacheDao
import sg.mesha.goatos.core.data.cache.ControlTowerCacheEntity
import sg.mesha.goatos.core.data.cache.CountsApprovalItemDao
import sg.mesha.goatos.core.data.cache.CountsApprovalItemEntity
import sg.mesha.goatos.core.data.cache.CountsApprovalRemoteKeyDao
import sg.mesha.goatos.core.data.cache.CountsApprovalRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownItemDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownItemEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownRemoteKeyDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheDao
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionItemDao
import sg.mesha.goatos.core.data.cache.FeedDirectionItemEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionRemoteKeyDao
import sg.mesha.goatos.core.data.cache.FeedDirectionRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.FeedPackingItemDao
import sg.mesha.goatos.core.data.cache.FeedPackingItemEntity
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedPackingRemoteKeyDao
import sg.mesha.goatos.core.data.cache.FeedPackingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.FeedTransportItemDao
import sg.mesha.goatos.core.data.cache.FeedTransportItemEntity
import sg.mesha.goatos.core.data.cache.FeedTransportRemoteKeyDao
import sg.mesha.goatos.core.data.cache.FeedTransportRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheDao
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheDao
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheDao
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheDao
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheDao
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.AwaitingRfidItemDao
import sg.mesha.goatos.core.data.cache.AwaitingRfidItemEntity
import sg.mesha.goatos.core.data.cache.AwaitingRfidRemoteKeyDao
import sg.mesha.goatos.core.data.cache.AwaitingRfidRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheDao
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.ShiftingPendingItemDao
import sg.mesha.goatos.core.data.cache.ShiftingPendingItemEntity
import sg.mesha.goatos.core.data.cache.ShiftingPendingRemoteKeyDao
import sg.mesha.goatos.core.data.cache.ShiftingPendingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheDao
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowCardDao
import sg.mesha.goatos.core.data.cache.WorkflowCardEntity
import sg.mesha.goatos.core.data.cache.WorkflowChipsCacheDao
import sg.mesha.goatos.core.data.cache.WorkflowChipsCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheDao
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowRemoteKeyDao
import sg.mesha.goatos.core.data.cache.WorkflowRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipGalleryRemoteKeyDao
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipGalleryRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordDao
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordRemoteKeyDao
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedDao
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedEntity
import sg.mesha.goatos.core.data.weighing.WeighingObservationDao
import sg.mesha.goatos.core.data.weighing.WeighingObservationEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalogDao
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperatorRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerParkRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochDao
import sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerRemoteKeyDao
import sg.mesha.goatos.core.data.weighing.WeighingPlannerRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShedRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingRosterDao
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketDao
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketRemoteKeyDao
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskDao
import sg.mesha.goatos.core.data.weighing.WeighingTaskRemoteKeyDao
import sg.mesha.goatos.core.data.weighing.WeighingTaskRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingShedObservationDao
import sg.mesha.goatos.core.data.weighing.WeighingShedObservationEntity

/**
 * The on-device SSOT database (docs/decisions/android-offline-first.md). v1 held only the
 * bootstrap cache; v2 (see [MIGRATION_1_2]) adds one JSON-blob-by-scope cache table per
 * screen-facing read model — Calendar, Control Tower, Execution (rows/shed/scan-roster),
 * Adherence, and Insights (gaps/coverage) — so every read screen observes Room instead of a
 * one-shot network call. v3 (see [MIGRATION_2_3]) adds the task-detail cache (MOB-001) so the
 * Scan -> Submit operator task read is offline-first too, not a network-only pass-through.
 * v4 (see [MIGRATION_3_4]) adds the scanned-goat + proof-capture tables (MOB-002) — the
 * Room-first SSOT behind Submit's `goat_scan`/`video_proof` recording-form controls. v5 (see
 * [MIGRATION_4_5]) adds the verification-queue cache — the standalone Verifier section's
 * category-filtered media queue (context/architecture/verifier-app-and-flow.md) offline-first
 * from day one, same as every other screen-facing read model. v6 (see [MIGRATION_5_6]) adds
 * backend goat/obligation ids to RFID scan captures so Submit can materialize completions by
 * goat id while still rendering/scanning RFID tags. v7 (see [MIGRATION_6_7]) adds an append-only
 * RFID attempt audit table so duplicate/alias/unknown physical reads sync without affecting
 * counters or Submit.
 * v8 (see [MIGRATION_7_8]) adds normalized Calendar monthly schedule rows and
 * backend keyset cursors for Paging 3; page data stays in Room instead of ViewModel memory.
 * v9 (see [MIGRATION_8_9]) adds normalized full-roster rows for indexed RFID lookup.
 * v10 (see [MIGRATION_9_10]) binds every vaccination clip to its goat and indexes the
 * per-goat upload/status view used on the shed scan screen.
 * v11 (see [MIGRATION_10_11]) scopes canonical RFID roster rows to shed+task.
 * v12 (see [MIGRATION_11_12]) adds the shed-completion-summary cache — the vaccination
 * shed-acknowledgement read the Submit screen renders, offline-first from day one like every
 * other screen-facing read model.
 * v13 (see [MIGRATION_12_13]) persists capture_source on proof_capture so startup-recovery
 * re-registration re-sends the original source instead of a default fallback.
 * v14 (see [MIGRATION_13_14]) makes the per-row scan_roster_row SSOT the sole source for the
 * shed scan screen (adds `seq` for a bounded keyset window and DROPS the scan_roster_cache blob).
 * v15 (see [MIGRATION_14_15]) adds the Counts vertical's read models in one step — the
 * herd-register summary rollup, the breakdown's fixed-size totals/charts/facets envelope,
 * normalized per-grain breakdown rows with their page offsets, the approver queue's keyset
 * rows and cursor, and the shifting destination catalog (the bounded park -> sheds vocabulary
 * behind the shifting screen's cascading dropdowns, so the picker still opens with real
 * options when the phone is offline in a shed). Renumbered from the branch's original v13 so
 * main's proof-capture (v13) and scan-roster SSOT (v14) migrations keep their numbers as the
 * integration baseline.
 * v16 (see [MIGRATION_15_16]) persists backend scan timestamps on scan_roster_row so reopened
 * vaccination tasks show the real done state and exact IST scan time.
 * v17 (see [MIGRATION_16_17]) adds the six Feed read-model tables — the Feed Direction sheet and
 * the Feed Packing worklist, each as a summary-envelope blob + normalized paged rows + per-scope
 * remote keys, so both Feed screens are offline-first and bounded from day one. Renumbered from the
 * branch's original v16 so main's scan-timestamp migration keeps v16 as the integration baseline.
 * v22 (see [MIGRATION_21_22]) adds Weighing's Room-first roster and category-aware observation
 * rows without touching Vaccination state. v23 (see [MIGRATION_22_23]) scopes Weighing's local
 * RFID uniqueness to the selected shed bucket so duplicate tags across buckets remain valid.
 */
@Database(
    entities = [
        BootstrapCacheEntity::class,
        CalendarCacheEntity::class,
        ControlTowerCacheEntity::class,
        ExecutionRowsCacheEntity::class,
        ExecutionShedCacheEntity::class,
        ScanRosterRowEntity::class,
        AdherenceCacheEntity::class,
        InsightsGapsCacheEntity::class,
        InsightsCoverageCacheEntity::class,
        RosterTimetableCacheEntity::class,
        RosterCoverageCacheEntity::class,
        TaskDetailCacheEntity::class,
        ShedCompletionSummaryCacheEntity::class,
        ScannedGoatEntity::class,
        RfidScanAttemptEntity::class,
        ProofCaptureEntity::class,
        VerificationQueueCacheEntity::class,
        CalendarScheduleEntity::class,
        CalendarScheduleRemoteKeyEntity::class,
        HerdSummaryCacheEntity::class,
        CountsBreakdownMetaCacheEntity::class,
        CountsBreakdownItemEntity::class,
        CountsBreakdownRemoteKeyEntity::class,
        CountsApprovalItemEntity::class,
        CountsApprovalRemoteKeyEntity::class,
        CountsShiftingDestinationsCacheEntity::class,
        FeedDirectionMetaCacheEntity::class,
        FeedDirectionItemEntity::class,
        FeedDirectionRemoteKeyEntity::class,
        FeedPackingMetaCacheEntity::class,
        FeedPackingItemEntity::class,
        FeedPackingRemoteKeyEntity::class,
        ShiftingPendingItemEntity::class,
        ShiftingPendingRemoteKeyEntity::class,
        AwaitingRfidItemEntity::class,
        AwaitingRfidRemoteKeyEntity::class,
        WorkflowCardEntity::class,
        WorkflowRemoteKeyEntity::class,
        WorkflowChipsCacheEntity::class,
        WorkflowDetailCacheEntity::class,
        FeedTransportItemEntity::class,
        FeedTransportRemoteKeyEntity::class,
        WeighingRosterRowEntity::class,
        WeighingObservationEntity::class,
        WeighingShedObservationEntity::class,
        WeighingTaskRowEntity::class,
        WeighingTaskRemoteKeyEntity::class,
        WeighingTaskBucketRowEntity::class,
        WeighingTaskBucketRemoteKeyEntity::class,
        WeighingLeadershipShedEntity::class,
        WeighingLeadershipRecordEntity::class,
        WeighingLeadershipRecordRemoteKeyEntity::class,
        WeighingLeadershipGalleryRemoteKeyEntity::class,
        WeighingPlannerParkRowEntity::class,
        WeighingPlannerShedRowEntity::class,
        WeighingPlannerOperatorRowEntity::class,
        WeighingPlannerRemoteKeyEntity::class,
        WeighingTransitionEpochEntity::class,
    ],
    version = 27,
    // exportSchema=true writes schemas/<db-fqcn>/<version>.json (see build.gradle.kts
    // room.schemaLocation). The committed schema JSON is the golden schema
    // MigrationTestHelper validates each migration against, and it makes every schema
    // change reviewable as a diff. A version bump with no new schema JSON is a red flag.
    // v9 (see [MIGRATION_8_9]) adds scan_roster_row entity for R50-007: full-roster tag lookup
    // via bounded indexed Room query (offline-first SSOT single-row entities instead of merged blob).
    // v10 (see [MIGRATION_9_10]) adds row-level vaccination proof ownership.
    // v11 (see [MIGRATION_10_11]) scopes canonical RFID rows to shed+task.
    // v12 (see [MIGRATION_11_12]) adds the shed-completion-summary cache table.
    // v13 (see [MIGRATION_12_13]) persists capture_source on proof_capture so startup-recovery
    // re-registration re-sends the original source instead of a default fallback.
    // v14 (see [MIGRATION_13_14]) makes the per-row scan_roster_row SSOT the sole source for the
    // shed scan screen: adds `seq` (backend roster order) so the UI renders a bounded keyset window
    // instead of a whole-collection JSON blob, and DROPS the now-unused scan_roster_cache blob table.
    // v15 (see [MIGRATION_14_15]) adds the seven Counts read-model tables.
    // v16 (see [MIGRATION_15_16]) adds nullable backend scan timestamp to scan_roster_row.
    // v17 (see [MIGRATION_16_17]) adds the six Feed read-model tables (Direction + Packing).
    // v18 (see [MIGRATION_17_18]) adds the shifting pending-execution queue pair — the Shifting
    // "Pending" tab's offline-first read model (one Room row per authorized movement + its keyset
    // remote key), so an operator re-entering the tab sees their cached queue instead of a blank wall.
    // v19 (see [MIGRATION_18_19]) adds the "Awaiting RFID" list pair — the Counts promote flow's
    // offline-first read model (one Room row per temporary-tagged goat + its keyset remote key), so an
    // operator re-entering the list sees their cached rows instead of a blank wall.
    // v20 (see [MIGRATION_19_20]) adds the four Birth/Death workflow read-model tables
    // (docs/decisions/birth-death-workflows.md): the keyset card list + its remote keys, the
    // per-day chips rollup, and the drill-in detail blob — the two new work-list modules
    // (/counts/birth, /counts/death) offline-first and bounded from day one.
    // v22 (see [MIGRATION_21_22]) adds Weighing's local roster and observation tables.
    // v23 (see [MIGRATION_22_23]) relaxes Weighing RFID uniqueness from campaign-wide to
    // selected shed-bucket-wide, matching the free-flow bucket model.
    // v25 (see [MIGRATION_24_25]) gives the leadership VIDEOS gallery its own cursor table, so a
    // surface's cursor is never stored in — and pruned by — another surface's table.
    // v26 (see [MIGRATION_25_26]) splits the planner catalog into its two real grains: a PARK table
    // (every park the planner may use on a date, with the backend's own park-grain shed COUNT) and
    // the existing shed table re-keyed per park. The two used to share one flattened keyset page,
    // so a 76-shed park filled page one alone and the wizard's park step offered a single park.
    // v24 (see [MIGRATION_23_24]) adds the ten Weighing LEADERSHIP read-model tables — the task
    // list, the task's shed buckets, the shed's own context and its captured records (shared with
    // the videos gallery), and the planner catalog — each a keyset-paged row table plus its remote
    // key, so every leadership surface renders from Room instead of straight off a network call.
    // v27 (see [MIGRATION_26_27]) persists the weighing per-scope idempotency EPOCH. It was an
    // in-heap map, so a Close whose response was lost on a phone that was then killed retried
    // under a NEW key and the backend applied a second close instead of replaying the first.
    exportSchema = true,
)
abstract class GoatDatabase : RoomDatabase() {
	abstract fun feedTransportItemDao(): FeedTransportItemDao
	abstract fun feedTransportRemoteKeyDao(): FeedTransportRemoteKeyDao
    abstract fun bootstrapCacheDao(): BootstrapCacheDao
    abstract fun calendarCacheDao(): CalendarCacheDao
    abstract fun calendarScheduleDao(): CalendarScheduleDao
    abstract fun calendarScheduleRemoteKeyDao(): CalendarScheduleRemoteKeyDao
    abstract fun controlTowerCacheDao(): ControlTowerCacheDao
    abstract fun executionRowsCacheDao(): ExecutionRowsCacheDao
    abstract fun executionShedCacheDao(): ExecutionShedCacheDao
    abstract fun scanRosterRowDao(): ScanRosterRowDao
    abstract fun adherenceCacheDao(): AdherenceCacheDao
    abstract fun insightsGapsCacheDao(): InsightsGapsCacheDao
    abstract fun insightsCoverageCacheDao(): InsightsCoverageCacheDao
    abstract fun rosterTimetableCacheDao(): RosterTimetableCacheDao
    abstract fun rosterCoverageCacheDao(): RosterCoverageCacheDao
    abstract fun taskDetailCacheDao(): TaskDetailCacheDao
    abstract fun shedCompletionSummaryCacheDao(): ShedCompletionSummaryCacheDao
    abstract fun scannedGoatDao(): ScannedGoatDao
    abstract fun rfidScanAttemptDao(): RfidScanAttemptDao
    abstract fun proofCaptureDao(): ProofCaptureDao
    abstract fun verificationQueueCacheDao(): VerificationQueueCacheDao
    abstract fun herdSummaryCacheDao(): HerdSummaryCacheDao
    abstract fun countsBreakdownMetaCacheDao(): CountsBreakdownMetaCacheDao
    abstract fun countsBreakdownItemDao(): CountsBreakdownItemDao
    abstract fun countsBreakdownRemoteKeyDao(): CountsBreakdownRemoteKeyDao
    abstract fun countsApprovalItemDao(): CountsApprovalItemDao
    abstract fun countsApprovalRemoteKeyDao(): CountsApprovalRemoteKeyDao
    abstract fun countsShiftingDestinationsCacheDao(): CountsShiftingDestinationsCacheDao
    abstract fun feedDirectionMetaCacheDao(): FeedDirectionMetaCacheDao
    abstract fun feedDirectionItemDao(): FeedDirectionItemDao
    abstract fun feedDirectionRemoteKeyDao(): FeedDirectionRemoteKeyDao
    abstract fun feedPackingMetaCacheDao(): FeedPackingMetaCacheDao
    abstract fun feedPackingItemDao(): FeedPackingItemDao
    abstract fun feedPackingRemoteKeyDao(): FeedPackingRemoteKeyDao
    abstract fun shiftingPendingItemDao(): ShiftingPendingItemDao
    abstract fun shiftingPendingRemoteKeyDao(): ShiftingPendingRemoteKeyDao
    abstract fun awaitingRfidItemDao(): AwaitingRfidItemDao
    abstract fun awaitingRfidRemoteKeyDao(): AwaitingRfidRemoteKeyDao
    abstract fun workflowCardDao(): WorkflowCardDao
    abstract fun workflowRemoteKeyDao(): WorkflowRemoteKeyDao
    abstract fun workflowChipsCacheDao(): WorkflowChipsCacheDao
    abstract fun workflowDetailCacheDao(): WorkflowDetailCacheDao
    abstract fun weighingRosterDao(): WeighingRosterDao
    abstract fun weighingObservationDao(): WeighingObservationDao
    abstract fun weighingShedObservationDao(): WeighingShedObservationDao
    abstract fun weighingTaskDao(): WeighingTaskDao
    abstract fun weighingTaskRemoteKeyDao(): WeighingTaskRemoteKeyDao
    abstract fun weighingTaskBucketDao(): WeighingTaskBucketDao
    abstract fun weighingTaskBucketRemoteKeyDao(): WeighingTaskBucketRemoteKeyDao
    abstract fun weighingLeadershipShedDao(): WeighingLeadershipShedDao
    abstract fun weighingLeadershipRecordDao(): WeighingLeadershipRecordDao
    abstract fun weighingLeadershipRecordRemoteKeyDao(): WeighingLeadershipRecordRemoteKeyDao

    abstract fun weighingLeadershipGalleryRemoteKeyDao(): WeighingLeadershipGalleryRemoteKeyDao
    abstract fun weighingPlannerCatalogDao(): WeighingPlannerCatalogDao
    abstract fun weighingPlannerRemoteKeyDao(): WeighingPlannerRemoteKeyDao
    abstract fun weighingTransitionEpochDao(): WeighingTransitionEpochDao
}
