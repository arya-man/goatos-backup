plugins {
    alias(libs.plugins.android.library)
}

android {
    namespace = "sg.mesha.goatos.core.analytics"
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
    implementation(project(":core:core-model"))
    // FirebasePerfNetworkTelemetryReporter implements core-network's NetworkTelemetryReporter
    // port (docs/TELEMETRY.md) — every Firebase-vendor adapter lives in this module.
    implementation(project(":core:core-network"))
    // FailureReportingOutboxTelemetryReporter implements core-common's OutboxTelemetryReporter
    // port — the queue-lifecycle twin of the network port above. core-common is Android-free and
    // does not depend on this module, so there is no cycle.
    implementation(project(":core:core-common"))

    // Real telemetry impls (FirebaseAnalyticsAdapter, FirebaseCrashReporter,
    // FirebasePerformanceTracer, FirebasePerfNetworkTelemetryReporter). NoopAnalytics /
    // NoopCrashReporter / NoopPerformanceTracer above have zero vendor dependency and remain
    // the fallback for tests/local (docs/TELEMETRY.md).
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.analytics)
    implementation(libs.firebase.crashlytics)
    implementation(libs.firebase.perf)

    testImplementation(libs.junit)
}
