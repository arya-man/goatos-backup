// telemetry:exempt: build config only, no product code — AnalyticsPort/AnalyticsFunnels wiring
// lives in WeighingViewModel/WeighingPlanWizardViewModel/WeighingShedDetailViewModel/
// WeighingLeadershipVideosViewModel (:app), see the screens in this module for their own
// telemetry:exempt (pure renderers).
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.feature.weighing"
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

    // DEVIATION from the "feature-* -> core-* only" rule in settings.gradle.kts.
    // WeighingScreen's execution surface reuses the scan surface's two shared UI value
    // types — `ScanReaderConnection` (reader status strip) and `ProofUploadStatus` (proof
    // sync chip). Both are plain UI enums/data holders declared in :feature:feature-scan.
    // Lifting them into :core:core-ui would touch feature-scan, which is out of scope for
    // this move; this dependency preserves behaviour byte-for-byte instead.
    implementation(project(":feature:feature-scan"))

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)

    // Streamed signed-URL playback for the leadership weighing-video review screen
    // (WeighingLeadershipVideosScreen). Capture-only CameraX stays in :app.
    // Players are built via ProofPlayerFactory (:core:core-media), NOT ExoPlayer.Builder
    // directly — that routes playback through the app's instrumented OkHttp client so a failed
    // proof-video fetch reaches the telemetry seam (W-22).
    implementation(project(":core:core-media"))
    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.ui)

    // Plain-JUnit coverage for the pure UI-state gates (WeighingUiState.submitBlockedReason):
    // a disabled Submit must always name its own reason, which is decidable off state alone.
    testImplementation("junit:junit:4.13.2")
}

// Compose compiler stability/metrics reports (item 6: perf/stability audit). Written under
// build/compose_metrics (*-classes.txt / *-composables.txt: stability per class/composable) and
// build/compose_reports (*-module.json). Regenerate with `./gradlew :<module>:assembleDevDebug`.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}
