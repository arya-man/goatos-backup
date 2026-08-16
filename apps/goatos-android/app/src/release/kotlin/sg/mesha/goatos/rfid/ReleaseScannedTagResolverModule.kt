package sg.mesha.goatos.rfid

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/**
 * Release-build binding for [ScannedTagResolver]: always the identity [PassthroughScannedTagResolver].
 * Zero trace of the debug sample-card fixture (see app/src/debug/.../DebugScannedTagResolverModule.kt)
 * ships in a release build — this build-type source set is the only place ScannedTagResolver is bound
 * for release, and it contains nothing but this passthrough.
 */
@Module
@InstallIn(SingletonComponent::class)
object ReleaseScannedTagResolverModule {
    @Provides
    fun provideScannedTagResolver(): ScannedTagResolver = PassthroughScannedTagResolver
}
