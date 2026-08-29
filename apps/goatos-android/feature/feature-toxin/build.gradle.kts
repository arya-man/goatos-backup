// telemetry:exempt build configuration, not a user-facing surface — it has no screen, no user
// action, and no error path to report. The module's real telemetry lives with the @HiltViewModels
// in :app (ToxinTaskListViewModel / ToxinTaskDetailViewModel), which wire AnalyticsEventsToxin
// events and CrashReporter for every read refresh, capture, and submit.

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.feature.toxin"
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
    // feature-* -> core-* only, never feature -> feature (settings.gradle.kts). No Hilt, no
    // networking, and no Room here: this module is a stateless renderer. State comes in as a
    // parameter and events go out through a callback; the @HiltViewModels that own the Room flows
    // and the paged reads live in :app.
    implementation(project(":core:core-designsystem"))
    implementation(project(":core:core-model"))
    implementation(project(":core:core-ui"))
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.compose.ui.tooling.preview)
    // Paging's Compose bindings: the task list renders LazyPagingItems so it is bounded at both
    // layers (Room window + network page), never a materialized full list.
    implementation(libs.androidx.paging.compose)
    // The strip photo is SHOWN back to the operator after capture — the step's deliverable is the
    // image, so seeing it is the confirmation, not a button label.
    implementation(libs.coil.compose)
    debugImplementation(libs.androidx.compose.ui.tooling)
    // Pure-JVM tests for the screen's own arming logic (which gate the refresh is scheduled off).
    testImplementation("junit:junit:4.13.2")
}

// Compose compiler stability/metrics reports, matching the other feature modules.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}
