// Root build — plugins declared (apply false) so modules opt in via the catalog.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.android.test) apply false
    alias(libs.plugins.androidx.baselineprofile) apply false
    alias(libs.plugins.kotlin.jvm) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
    alias(libs.plugins.paparazzi) apply false
    alias(libs.plugins.firebase.appdistribution) apply false
    // Observability (docs/TELEMETRY.md): converts google-services.json -> FirebaseOptions
    // resources, then Crashlytics + Performance Monitoring bytecode instrumentation.
    alias(libs.plugins.google.services) apply false
    alias(libs.plugins.firebase.crashlytics) apply false
    alias(libs.plugins.firebase.perf) apply false
}
