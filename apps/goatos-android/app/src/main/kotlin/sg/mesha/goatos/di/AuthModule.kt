package sg.mesha.goatos.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.auth.FirebaseAuthRepository
import javax.inject.Singleton

/** Firebase Auth repository wiring for stg/prod SSO and email/password sign-in. */
@Module
@InstallIn(SingletonComponent::class)
object AuthModule {

    @Provides
    @Singleton
    fun provideAuthRepository(): AuthRepository = FirebaseAuthRepository()
}
