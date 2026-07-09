plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
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
}

dependencies {
    api(project(":core:core-model"))
    // `api` (not `implementation`): the screen repositories below return core-network
    // DTOs from their public methods, so those types must be on the consumer (:app)
    // compile classpath.
    api(project(":core:core-network"))
    implementation(project(":core:core-common"))
    implementation(libs.kotlinx.coroutines.core)
    implementation(libs.kotlinx.serialization.json)
    api(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    // Proto DataStore + the WorkManager sync/outbox engine land here next; the Room
    // cache below gives offline-first bootstrap today.
}
