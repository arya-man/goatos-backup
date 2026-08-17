package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow

/**
 * The grains whose badge should currently read "in review", i.e. every submit still alive in the
 * outbox.
 *
 * A one-method port on purpose: a list ViewModel needs exactly this and nothing else from the sync
 * layer, and a narrow port keeps the fake in a test to one line instead of the whole SyncRepository
 * surface. Production binds it to [SyncRepository.observeSubmittedForReviewGrains].
 */
fun interface SubmittedGrainsSource {
    fun observe(): Flow<Set<String>>
}
