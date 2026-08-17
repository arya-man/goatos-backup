// Shared proof-video trim editor. ADDITIVE module: nothing existing depends on it until a
// capture host opts in behind the editing feature flag, so with the flag off the proof pipeline
// is byte-identical to before this module existed.
//
// It lives here rather than in :core:core-media because that module is deliberately Compose
// RUNTIME only (no ui/material) — see its build file — and this one owns a full editor surface.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "sg.mesha.goatos.core.proofedit"
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
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons.extended)
    implementation(libs.androidx.lifecycle.runtime.compose)
    // BackHandler: the editor MUST intercept system back (see ProofTrimEditor).
    implementation(libs.androidx.activity.compose)

    // Trim + stitch is one media3 Transformer export; the preview player is ExoPlayer.
    api(libs.androidx.media3.exoplayer)
    api(libs.androidx.media3.ui)
    api(libs.androidx.media3.transformer)
    api(libs.androidx.media3.effect)

    testImplementation("junit:junit:4.13.2")
}
