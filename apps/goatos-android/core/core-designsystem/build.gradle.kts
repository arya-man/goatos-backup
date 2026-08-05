plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.core.designsystem"
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
    implementation(platform(libs.androidx.compose.bom))
    api(libs.androidx.compose.material3)
    api(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)

    // Plain-JUnit coverage for MeshaIcons.forNavKey (pure String -> ImageVector mapping, no
    // Compose runtime needed) -- see feature-verify's own testImplementation("junit") for the
    // same pattern.
    testImplementation("junit:junit:4.13.2")
}

// Compose compiler stability/metrics reports (item 6: perf/stability audit). Written under
// build/compose_metrics (*-classes.txt / *-composables.txt: stability per class/composable) and
// build/compose_reports (*-module.json). Regenerate with `./gradlew :<module>:assembleDevDebug`.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}
