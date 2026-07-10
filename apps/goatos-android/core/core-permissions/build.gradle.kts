plugins {
    alias(libs.plugins.android.library)
}

// Login-time permission-gate logic (docs/mobile/rfid-keyboard-reader.md permission
// matrix + docs/mobile/trd-operator-mobile.md §7). Android-only for Build.VERSION /
// Manifest.permission constants; no Compose here by design — feature-auth owns the
// rationale UI + AndroidX Activity Result launcher (TRD §3 module boundaries).
android {
    namespace = "sg.mesha.goatos.core.permissions"
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
    implementation(libs.androidx.core.ktx)
    testImplementation("junit:junit:4.13.2")
}
