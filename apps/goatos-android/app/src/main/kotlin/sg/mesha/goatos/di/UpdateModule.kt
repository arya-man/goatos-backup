package sg.mesha.goatos.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.update.RemoteConfigUpdateGate
import sg.mesha.goatos.update.UpdateGate
import javax.inject.Singleton

/** Force-update gate wiring (Firebase Remote Config adapter). */
@Module
@InstallIn(SingletonComponent::class)
object UpdateModule {

    @Provides
    @Singleton
    fun provideUpdateGate(): UpdateGate = RemoteConfigUpdateGate()
}
