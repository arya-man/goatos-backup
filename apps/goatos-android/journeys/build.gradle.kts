// Lane 5 — the farm-journey instrumentation suite that runs on Firebase Test Lab
// VIRTUAL devices (docs/engineering/lane5-android.md).
//
// Shaped like :benchmark — a com.android.test module that self-instruments against
// :app — with one deliberate difference: it builds against the DEBUG build type, not
// `benchmark`. The app's debug-only affordances this suite drives
// (app/src/debug/.../DebugNavigationReceiver.kt and DebugRfidInjectionReceiver.kt)
// merge into `debug` only, and `benchmark` initWith(release) does not see them. That
// is exactly why :benchmark has to tap backend-composed nav labels and needs a live
// bootstrap; this suite does not.
plugins {
    alias(libs.plugins.android.test)
}

android {
    namespace = "sg.mesha.goatos.journeys"
    compileSdk = 36

    defaultConfig {
        minSdk = 29
        targetSdk = 36
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    targetProjectPath = ":app"
    experimentalProperties["android.experimental.self-instrumenting"] = true

    flavorDimensions += "env"
    productFlavors {
        create("dev") { dimension = "env" }
        create("stg") { dimension = "env" }
        create("prod") { dimension = "env" }
    }
}

dependencies {
    implementation(libs.androidx.test.runner)
    implementation(libs.androidx.test.ext.junit)
    implementation(libs.androidx.test.uiautomator)
}
