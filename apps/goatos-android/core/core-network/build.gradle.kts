plugins {
    alias(libs.plugins.android.library)
}

android {
    namespace = "sg.mesha.goatos.core.network"
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
    implementation(project(":core:core-common"))
    implementation(libs.kotlinx.coroutines.core)
    // Retrofit 3 / OkHttp 5 + kotlinx.serialization + the OpenAPI-generated Kotlin
    // client land here in the network pass; the AppApi port + FakeAppApi below keep
    // the shell running without a live backend or auth.
}
