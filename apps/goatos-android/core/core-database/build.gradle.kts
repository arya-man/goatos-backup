plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
}

// Pure persistence layer (TRD §3 mirrors core-datastore's independence): the local
// outbox Room schema only. No project(":core:...") deps on purpose — SyncEngine /
// SyncRepository / OutboxStore (the actual sync business logic + the port other
// modules consume) live in :core:core-data, which depends on this module. Keeping
// this module dependency-free maximizes reuse and keeps the outbox schema testable
// in isolation.
android {
    namespace = "sg.mesha.goatos.core.database"
    compileSdk = 36
    defaultConfig {
        minSdk = 29
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(libs.kotlinx.coroutines.core)
    api(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
}
