// telemetry:exempt build configuration, not a user-facing surface — it has no screen, no user
// action, and no error path to report. The module's real telemetry lives with the @HiltViewModels
// in :app (FeedDirectionViewModel / FeedPackingViewModel), which wire AnalyticsPort events for
// every read refresh and filter change.

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.feature.feed"
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
    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.ui)
    // Paging's Compose bindings: the two feed read screens render LazyPagingItems so each list is
    // bounded at both layers (Room window + network page), never a materialized full sheet.
    implementation(libs.androidx.paging.compose)
    debugImplementation(libs.androidx.compose.ui.tooling)
}

// Compose compiler stability/metrics reports, matching the other feature modules.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}
