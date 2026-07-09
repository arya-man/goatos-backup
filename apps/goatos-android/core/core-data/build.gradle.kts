plugins {
    alias(libs.plugins.android.library)
}

android {
    namespace = "sg.mesha.goatos.core.data"
    compileSdk = 36
    defaultConfig {
        minSdk = 31
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    api(project(":core:core-model"))
    implementation(project(":core:core-network"))
    implementation(project(":core:core-common"))
    implementation(libs.kotlinx.coroutines.core)
    // Room + Proto DataStore + the sync/outbox engine (with fakes) land here in the
    // data pass; repositories expose Flow and cache the bootstrap by ETag/revision.
}
