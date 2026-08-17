// telemetry:exempt: build config only, no product code — AnalyticsPort/AnalyticsEvents wiring for
// Health lives in AddHealthCaseViewModel (HEALTH_CASE_SUBMITTED / HEALTH_WRITE_FAILURE /
// HEALTH_READ_FAILURE).
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.feature.health"
    compileSdk = 36
    defaultConfig { minSdk = 29 }
    buildFeatures { compose = true }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(project(":core:core-designsystem"))
    implementation(project(":core:core-ui"))
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.foundation)
    // BackHandler. The observation form is walked in steps, so system back must
    // step BACKWARDS through it before it leaves — otherwise back on the last step
    // throws away every answer already recorded on the animal.
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.paging.compose)
    debugImplementation(libs.androidx.compose.ui.tooling)
    testImplementation(libs.junit)
}
