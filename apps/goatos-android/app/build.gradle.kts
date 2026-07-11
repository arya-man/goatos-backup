import com.android.build.api.variant.HasHostTestsBuilder
import com.android.build.api.variant.HostTestBuilder

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.hilt)
    alias(libs.plugins.ksp)
    alias(libs.plugins.paparazzi)
    alias(libs.plugins.firebase.appdistribution)
}

android {
    namespace = "sg.mesha.goatos"
    compileSdk = 36

    defaultConfig {
        applicationId = "sg.mesha.goatos"
        minSdk = 29
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        // Local dev bearer token (a minted HS256 dev token), injected from a gradle
        // property so it's NEVER committed: -PgoatosDevBearerToken=... or in
        // ~/.gradle/gradle.properties / local.properties. Empty in prod (real
        // Firebase token drives auth there). The dev flow seeds this as the session
        // token so the app authenticates against the local backend.
        val devToken = (project.findProperty("goatosDevBearerToken") as String?).orEmpty()
        val tenantId = (project.findProperty("goatosTenantId") as String?)
            ?: "00000000-0000-4000-8000-000000000001"
        val authActionLinkDomain = (project.findProperty("goatosAuthActionLinkDomain") as String?).orEmpty()
        buildConfigField("String", "DEV_BEARER_TOKEN", "\"$devToken\"")
        buildConfigField("String", "TENANT_ID", "\"$tenantId\"")
        buildConfigField("String", "AUTH_ACTION_LINK_DOMAIN", "\"${authActionLinkDomain.replace("\"", "\\\"")}\"")
    }

    // One common app; env is a build flavor, roles are runtime (app-id ADR).
    // API_BASE_URL is per-flavor: dev → local backend (10.0.2.2 = host from the
    // Android emulator); stg/prod point at the deployed API (TBD).
    flavorDimensions += "env"
    productFlavors {
        create("dev") {
            dimension = "env"
            applicationIdSuffix = ".dev"
            versionNameSuffix = "-dev"
            buildConfigField("String", "API_BASE_URL", "\"http://10.0.2.2:8080/\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"http://localhost:3311/login\"")
        }
        create("stg") {
            dimension = "env"
            applicationIdSuffix = ".stg"
            versionNameSuffix = "-stg"
            buildConfigField("String", "API_BASE_URL", "\"https://goatos-api-stg-514832198871.asia-south1.run.app/\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"https://stg.dashboard.mesha.sg/login\"")

            // Firebase App Distribution — ships stg builds to testers. appId is the
            // registered goatos-stg Android client. Testers live in the Firebase
            // "goatos-testers" group (emails stay in the console, never the repo);
            // override per-invoke with -PfadGroups / -PfadTesters / -PfadReleaseNotes.
            firebaseAppDistribution {
                appId = "1:514832198871:android:0cb898377ba4f7f7f19492"
                artifactType = "APK"
                groups = (project.findProperty("fadGroups") as String?) ?: "goatos-testers"
                (project.findProperty("fadTesters") as String?)?.let { testers = it }
                releaseNotes = (project.findProperty("fadReleaseNotes") as String?)
                    ?: "Goat OS (Mesha) stg release build"
            }
        }
        create("prod") {
            dimension = "env"
            buildConfigField("String", "API_BASE_URL", "\"https://api.goatos.mesha.sg/\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"https://dashboard.mesha.sg/login\"")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(project(":core:core-designsystem"))
    implementation(project(":core:core-model"))
    implementation(project(":core:core-common"))
    implementation(project(":core:core-network"))
    implementation(project(":core:core-data"))
    // CoverageBannerUiState (shared across feature-calendar + :app's CoverageBannerViewModel).
    implementation(project(":core:core-ui"))
    // Calendar event `links` arrive as Map<String, JsonElement> from core-network's DTO;
    // the ViewModel reads route hrefs off them via jsonPrimitive/contentOrNull.
    implementation(libs.kotlinx.serialization.json)
    implementation(project(":core:core-datastore"))
    implementation(project(":feature:feature-auth"))
    implementation(project(":feature:feature-calendar"))
    implementation(project(":feature:feature-sheds"))
    implementation(project(":feature:feature-scan"))
    implementation(project(":feature:feature-submit"))
    implementation(project(":feature:feature-leadership"))
    implementation(project(":feature:feature-record"))
    implementation(project(":feature:feature-profile"))
    implementation(project(":feature:feature-timetable"))

    implementation(libs.hilt.android)
    implementation(libs.hilt.navigation.compose)
    ksp(libs.hilt.compiler)

    // WorkManager + Hilt worker factory: guarantees the outbox drains on reconnect even after the
    // process is killed (an OS-scheduled job survives process death, which the in-process
    // connectivity trigger cannot). SyncWorker.doWork() delegates to the same SyncEngine.drainOnce().
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.hilt.work)
    ksp(libs.androidx.hilt.compiler)

    // Runtime memory-leak detector — debug builds only, never shipped in release. Auto-watches
    // destroyed Activities/Fragments/ViewModels and dumps a leak trace if any is retained.
    debugImplementation(libs.leakcanary.android)

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)

    // Firebase Auth + Credential Manager for stg/prod mobile SSO. Firebase options for
    // stg are committed as generated-equivalent string resources under src/stg/res.
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.auth)
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services.auth)
    implementation(libs.googleid)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)

    // Virtual-time coroutine testing (runTest/advanceTimeBy) for the offline-banner debounce.
    testImplementation(libs.kotlinx.coroutines.test)
}

// Compose compiler stability/metrics reports (item 6: perf/stability audit). Written under
// build/compose_metrics (*-classes.txt / *-composables.txt: stability per class/composable) and
// build/compose_reports (*-module.json). Regenerate with `./gradlew :<module>:assembleDevDebug`.
composeCompiler {
    metricsDestination = layout.buildDirectory.dir("compose_metrics")
    reportsDestination = layout.buildDirectory.dir("compose_reports")
}

androidComponents {
    beforeVariants(
        selector()
            .withBuildType("release")
            .withFlavor("env" to "stg"),
    ) { variantBuilder ->
        (variantBuilder as HasHostTestsBuilder)
            .hostTests
            .get(HostTestBuilder.UNIT_TEST_TYPE)
            ?.enable = true
    }
}
