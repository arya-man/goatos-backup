plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
    // Needed to generate serializers for the outbox's own @Serializable payload models
    // (sg.mesha.goatos.core.data.sync.SyncPayloads) — this module already USES
    // kotlinx.serialization.json.Json (BootstrapCache) but never applied the compiler
    // plugin itself (the @Serializable types it decoded were all defined in core-network,
    // which already applies this plugin).
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "sg.mesha.goatos.core.data"
    compileSdk = 36
    defaultConfig {
        minSdk = 29
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    // Robolectric-backed unit tests (GoatDatabaseCacheTest) need a real Context, so JUnit
    // can resolve Robolectric's own resources/manifest at test runtime.
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            // LogoutCoordinator (C35-001) logs a best-effort failure via android.util.Log on
            // its plain-JUnit (non-Robolectric) test path; without this, any unmocked
            // android.* call throws instead of no-op'ing under the plain unit-test runner.
            isReturnDefaultValues = true
        }
    }
}

dependencies {
    api(project(":core:core-model"))
    // `api` (not `implementation`): the screen repositories below return core-network
    // DTOs from their public methods, so those types must be on the consumer (:app)
    // compile classpath.
    api(project(":core:core-network"))
    implementation(project(":core:core-common"))
    // `api`: DefaultBootstrapRepository's public constructor exposes DeviceStore, so the
    // core-datastore type is part of core-data's ABI and must be on the consumer classpath.
    api(project(":core:core-datastore"))
    // `api`: AppModule's @Provides functions (in :app) construct/return OutboxDatabase /
    // OutboxDao directly (mirrors the GoatDatabase/BootstrapCacheDao pattern above), so
    // core-database's Room types must be on the consumer (:app) compile classpath.
    api(project(":core:core-database"))
    implementation(libs.kotlinx.coroutines.core)
    implementation(libs.kotlinx.serialization.json)
    api(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    // Proto DataStore lands next. The outbox/sync engine (SyncEngine, SyncRepository,
    // OutboxStore — sg.mesha.goatos.core.data.sync) stays framework-free here; the app module
    // wires WorkManager as a thin trigger/backstop around SyncEngine.drainOnce().
    testImplementation("junit:junit:4.13.2")
    testImplementation(libs.kotlinx.coroutines.test)
    // Room migration + cache round-trip test (GoatDatabaseCacheTest) needs a real
    // (shadowed) android.database.sqlite + Context — see the libs.versions.toml note.
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
}
