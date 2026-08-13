// telemetry:exempt: this module IS the media telemetry wiring — it hands media3 the app's
// OkHttp client (which already carries TelemetryInterceptor) rather than reporting anything
// itself. See ProofMediaHttp.kt.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.core.media"
    compileSdk = 36
    defaultConfig {
        minSdk = 29
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    // media3's DataSource/DataSpec plumbing touches android.util.Log and needs a real Context,
    // so the blind-spot proof runs under Robolectric.
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            isReturnDefaultValues = true
        }
    }
}

dependencies {
    api(project(":core:core-network"))
    implementation(platform(libs.androidx.compose.bom))
    // Compose RUNTIME only (no ui/material) — this module contributes one CompositionLocal so a
    // screen can obtain the telemetry-instrumented player factory without a DI graph of its own.
    api("androidx.compose.runtime:runtime")
    api(libs.androidx.media3.exoplayer)
    api(libs.androidx.media3.datasource.okhttp)
    implementation(libs.okhttp)

    testImplementation("junit:junit:4.13.2")
    testImplementation(kotlin("test"))
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
}
