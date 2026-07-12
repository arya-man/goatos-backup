// telemetry:exempt: build config only, no product code — AnalyticsPort/AnalyticsFunnels wiring
// lives in VerifyQueueViewModel/VerifyDetailViewModel (:app), see VerifyQueueScreen.kt /
// VerifyDetailScreen.kt in this module for their own telemetry:exempt (pure renderers).
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.feature.verify"
    compileSdk = 36
    defaultConfig {
        minSdk = 29
    }
    buildFeatures {
        compose = true
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(project(":core:core-designsystem"))
    implementation(project(":core:core-model"))
    implementation(project(":core:core-ui"))
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)

    // Streamed signed-URL video playback for the verification queue (verifier-app-and-flow.md).
    // Capture-only CameraX lives in :device:device-camera; this feature is playback-only.
    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.ui)
}

// Compose compiler stability/metrics reports (item 6: perf/stability audit). Written under
// build/compose_metrics (*-classes.txt / *-composables.txt: stability per class/composable) and
// build/compose_reports (*-module.json). Regenerate with `./gradlew :<module>:assembleDevDebug`.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}
