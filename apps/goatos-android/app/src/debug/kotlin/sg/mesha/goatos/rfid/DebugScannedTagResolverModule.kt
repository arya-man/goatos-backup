package sg.mesha.goatos.rfid

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/**
 * Debug-build binding for [ScannedTagResolver]: the real sample-card fixture, [DebugSampleTagAliaser].
 * This module lives only in the debug build-type source set, so it (and the aliaser) are compiled
 * into debug builds only — a release build has no ScannedTagResolver binding here at all, and instead
 * picks up ReleaseScannedTagResolverModule's identity binding from app/src/release/.
 */
@Module
@InstallIn(SingletonComponent::class)
abstract class DebugScannedTagResolverModule {
    @Binds
    abstract fun bindScannedTagResolver(impl: DebugSampleTagAliaser): ScannedTagResolver
}
