// telemetry:exempt pure state/step model for the create wizard — data classes and step ordering,
// no user-facing surface and no I/O. AnalyticsPort wiring lives in the wizard's ViewModel.
package sg.mesha.goatos.feature.weighing.plan

/**
 * The five steps of authoring one weighing task, in order.
 *
 * Review is a real step, not a confirmation popup: nothing publishes without passing through it.
 */
enum class WeighingWizardStep { DATE, PARK, BUCKETS, CONFIGURE, REVIEW }

/** How the shed-bucket list is narrowed while picking. */
enum class WeighingBucketFilter { AVAILABLE, TAKEN, ADDED, ALL }

data class WeighingWizardDateOption(
    val isoDate: String,
    val label: String,
    val note: String,
    val selected: Boolean,
)

data class WeighingWizardParkOption(
    val parkId: String,
    val name: String,
    val subtitle: String,
    val selected: Boolean,
)

/**
 * One shed on the pick step. [taken] means the SERVER says an open task already holds this shed on
 * the chosen weigh date; [reason] is the sentence that explains it.
 */
data class WeighingWizardBucketRow(
    val locationId: String,
    val name: String,
    val reason: String,
    val taken: Boolean,
    val added: Boolean,
)

data class WeighingWizardConfigRow(
    val locationId: String,
    val name: String,
    val estimateLabel: String,
    val category: String,
    val operatorUserId: String,
    val operatorLabel: String,
    val ticked: Boolean,
)

/** One operator's share of the configured buckets: WHO, and HOW MANY shed buckets are theirs. */
data class WeighingWizardOperatorLoad(
    val displayName: String,
    val shedCount: Int,
)

data class WeighingWizardOperatorOption(
    val userId: String,
    val displayName: String,
)

data class WeighingWizardReviewRow(
    /**
     * The selected shed's location id. This is the list key, NOT [name]: a shed name is neither
     * unique nor guaranteed non-blank, and two selected sheds missing from the catalog both key on
     * the empty string, which makes the review step -- the moment of publishing -- throw.
     */
    val locationId: String,
    val name: String,
    val categoryLabel: String,
    val operatorLabel: String,
)

/**
 * Everything the wizard renders, already derived from the planner's answers.
 *
 * Counts here count BUCKETS, never animals. Weighing capture is free-flow and has no expected
 * roster, so an animal-grain number would be invented.
 */
data class WeighingWizardUiState(
    val step: WeighingWizardStep = WeighingWizardStep.DATE,
    val stepCount: Int = WeighingWizardStep.entries.size,
    val loading: Boolean = false,
    val busy: Boolean = false,
    val message: String? = null,
    /** Set once the task exists on the server. The screen leaves the wizard when it appears. */
    val savedCampaignId: String? = null,
    val canContinue: Boolean = false,
    val contextLine: String = "",

    val dateOptions: List<WeighingWizardDateOption> = emptyList(),
    val selectedDate: String? = null,
    val dateLabel: String = "",

    val parkOptions: List<WeighingWizardParkOption> = emptyList(),
    val selectedParkId: String? = null,
    val parkName: String = "",

    val bucketQuery: String = "",
    val bucketFilter: WeighingBucketFilter = WeighingBucketFilter.AVAILABLE,
    val availableCount: Int = 0,
    val takenCount: Int = 0,
    val addedCount: Int = 0,
    val allCount: Int = 0,
    val bucketRows: List<WeighingWizardBucketRow> = emptyList(),
    val bucketShownCount: Int = 0,
    val bucketTotalCount: Int = 0,
    val bucketsAddable: Int = 0,
    val addedTray: List<String> = emptyList(),
    val addedTrayMore: Int = 0,

    val configQuery: String = "",
    val configSearchOpen: Boolean = false,
    val configRows: List<WeighingWizardConfigRow> = emptyList(),
    val configShownCount: Int = 0,
    val configTotalCount: Int = 0,
    val tickedCount: Int = 0,
    val operators: List<WeighingWizardOperatorOption> = emptyList(),
    /**
     * The configure step's summary, as STRUCTURED COUNTS rather than a pre-built sentence.
     *
     * It used to be assembled in the ViewModel with hardcoded English (" individual · ",
     * "lump-sum", "Operator") and a bare name+number pair, which rendered "Dinakar 2" -- a
     * name welded to a count with no unit, on a screen where the number could plausibly be
     * animals, sheds, or days. Counts stay numbers here; the screen names their unit, in the
     * reader's own language.
     */
    val configIndividualCount: Int = 0,
    val configLumpSumCount: Int = 0,
    val configPerOperator: List<WeighingWizardOperatorLoad> = emptyList(),

    /**
     * Set only when this task is being started FROM an existing one: names where the answers came
     * from, so the planner knows what is already filled in and can still change all of it.
     */
    val repeatSourceLabel: String? = null,
    /**
     * Buckets carried over that could NOT be added on the chosen date, because the server says
     * they are already scheduled that day or the park no longer has them. Stated rather than
     * silently dropped.
     */
    val repeatDroppedCount: Int = 0,

    val reviewRows: List<WeighingWizardReviewRow> = emptyList(),
    val reviewOperatorLabel: String = "",
    val lopsidedOperatorLabel: String? = null,
)
