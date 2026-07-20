import com.android.build.api.variant.HasHostTestsBuilder
import com.android.build.api.variant.HostTestBuilder
import com.google.firebase.appdistribution.gradle.firebaseAppDistribution

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.hilt)
    alias(libs.plugins.ksp)
    alias(libs.plugins.paparazzi)
    alias(libs.plugins.firebase.appdistribution)
    alias(libs.plugins.androidx.baselineprofile)
    // Observability (docs/TELEMETRY.md). google-services processes each flavor's
    // src/<flavor>/google-services.json into FirebaseOptions resources; Crashlytics +
    // Performance Monitoring instrument the assembled APK's bytecode (their automatic
    // screen/network traces need no code — see docs/TELEMETRY.md).
    alias(libs.plugins.google.services)
    alias(libs.plugins.firebase.crashlytics)
    alias(libs.plugins.firebase.perf)
}

android {
    namespace = "sg.mesha.goatos"
    compileSdk = 36

    fun signingProperty(name: String): String? =
        (project.findProperty(name) as String?)
            ?: System.getenv(name)

    signingConfigs {
        create("stgRelease") {
            val storeFilePath = signingProperty("GOATOS_ANDROID_STG_KEYSTORE")
            if (!storeFilePath.isNullOrBlank()) {
                storeFile = file(storeFilePath)
                storePassword = signingProperty("GOATOS_ANDROID_STG_KEYSTORE_PASSWORD")
                keyAlias = signingProperty("GOATOS_ANDROID_STG_KEY_ALIAS")
                keyPassword = signingProperty("GOATOS_ANDROID_STG_KEY_PASSWORD")
            }
        }
    }

    defaultConfig {
        applicationId = "sg.mesha.goatos"
        minSdk = 29
        targetSdk = 36
        versionCode = 2
        versionName = "0.1.1"

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
    // API_BASE_URL is per-flavor: dev → local backend; stg/prod point at the deployed
    // API (TBD). localhost:8080 works on BOTH the emulator and a USB-tethered physical
    // device when `adb reverse tcp:8080 tcp:8080` tunnels device-loopback → laptop.
    // (Was 10.0.2.2 = emulator-only host alias; localhost + adb reverse covers both.)
    flavorDimensions += "env"
    val devApiBaseUrl = (project.findProperty("goatosDevApiBaseUrl") as String?)
        ?: "http://localhost:8080/"
    productFlavors {
        create("dev") {
            dimension = "env"
            applicationIdSuffix = ".dev"
            versionNameSuffix = "-dev"
            buildConfigField("String", "API_BASE_URL", "\"${devApiBaseUrl.replace("\"", "\\\"")}\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"http://localhost:3311/login\"")
            // Telemetry (docs/TELEMETRY.md): ON for dev. The dev Android client is registered in
            // the goatos-stg Firebase PROJECT (app/src/dev/res/values/firebase.xml → project_id
            // goatos-stg, app sg.mesha.goatos.dev), so dev analytics/crashlytics/perf report there
            // under that app registration — separate from the stg app's own stream. This is also
            // what surfaces LeakCanary memory leaks: the debug-only leak→Crashlytics bridge
            // (app/src/debug/.../leak) only uploads when this reporter is the real Firebase one.
            // Flip off per-invoke with -PgoatosTelemetryEnabled=false for a fully offline local run.
            buildConfigField(
                "boolean",
                "TELEMETRY_ENABLED",
                (project.findProperty("goatosTelemetryEnabled") as String?) ?: "true",
            )
            // OTLP Collector endpoint — NOT deployed yet (OBSERVABILITY_DESIGN.md §6 rollout is
            // stg-first). TelemetryInterceptor only stamps traceparent + reports to Firebase Perf
            // today; this field is reserved for the OTel-Android OTLP exporter TODO.
            buildConfigField("String", "OTLP_ENDPOINT", "\"\"")
        }
        create("stg") {
            dimension = "env"
            applicationIdSuffix = ".stg"
            versionNameSuffix = "-stg"
            buildConfigField("String", "API_BASE_URL", "\"https://stg-api.dashboard.mesha.sg/\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"https://stg.dashboard.mesha.sg/login\"")
            // Telemetry (docs/TELEMETRY.md): stg has a CONFIRMED real Firebase project
            // (goatos-stg — see app/src/stg/res/values/firebase.xml + app/src/stg/google-services.json,
            // both reconstructed from the same committed real values). Rollout target (§6).
            buildConfigField("boolean", "TELEMETRY_ENABLED", "true")
            // OTel Collector Cloud Run service MUST be provisioned in asia-south1 (same region as
            // API_BASE_URL above and OBSERVABILITY_DESIGN.md's chosen stg region). Not deployed yet —
            // empty until infra lands; see docs/TELEMETRY.md "India region / asia-south1" section.
            buildConfigField("String", "OTLP_ENDPOINT", "\"\"")
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
            // Telemetry (docs/TELEMETRY.md): OFF until prod's real Firebase project is confirmed
            // and its google-services.json replaces the PLACEHOLDER at app/src/prod/google-services.json
            // (OBSERVABILITY_DESIGN.md §6: "prod needs its Layer-1 terraform foundation before enabling").
            buildConfigField(
                "boolean",
                "TELEMETRY_ENABLED",
                (project.findProperty("goatosTelemetryEnabled") as String?) ?: "false",
            )
            buildConfigField("String", "OTLP_ENDPOINT", "\"\"")
        }
    }

    buildTypes {
        debug {
            manifestPlaceholders["appLabel"] = "Mesha Debug"
        }
        release {
            isMinifyEnabled = false
            manifestPlaceholders["appLabel"] = "Mesha"
            val stgReleaseSigning = signingConfigs.getByName("stgRelease")
            if (stgReleaseSigning.storeFile != null) {
                signingConfig = stgReleaseSigning
            }
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
        create("benchmark") {
            initWith(getByName("release"))
            isDebuggable = false
            isProfileable = true
            manifestPlaceholders["appLabel"] = "Mesha"
            signingConfig = signingConfigs.getByName("debug")
            matchingFallbacks += listOf("release")
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
    // core-network's own OkHttp dependency is `implementation`-scoped (not exposed
    // transitively); :app needs the `okhttp3.Interceptor` type directly to construct
    // TelemetryInterceptor when wiring NetworkFactory.appApi (docs/TELEMETRY.md).
    implementation(libs.okhttp)
    implementation(project(":core:core-data"))
    // CoverageBannerUiState (shared across feature-calendar + :app's CoverageBannerViewModel).
    implementation(project(":core:core-ui"))
    // Calendar event `links` arrive as Map<String, JsonElement> from core-network's DTO;
    // the ViewModel reads route hrefs off them via jsonPrimitive/contentOrNull.
    implementation(libs.kotlinx.serialization.json)
    implementation(project(":core:core-datastore"))
    implementation(project(":feature:feature-auth"))
    implementation(project(":core:core-analytics"))

    implementation(project(":feature:feature-calendar"))
    implementation(project(":feature:feature-counts"))
    implementation(project(":feature:feature-sheds"))
    implementation(project(":feature:feature-scan"))
    implementation(project(":feature:feature-submit"))
    implementation(project(":feature:feature-leadership"))
    implementation(project(":feature:feature-record"))
    implementation(project(":feature:feature-profile"))
    implementation(project(":feature:feature-timetable"))
    implementation(project(":feature:feature-verify"))

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
    implementation(libs.androidx.paging.compose)
    baselineProfile(project(":benchmark"))

    // Firebase Auth + Credential Manager for stg/prod mobile SSO. Firebase options for
    // stg/dev are committed as generated-equivalent string resources under src/<flavor>/res
    // (prod ships none yet). firebase-messaging (GoatOsMessagingService push delivery) and
    // firebase-analytics (identity setUserId/setUserProperty) share the same BOM and the same
    // manual-firebase.xml init pattern — no google-services plugin, see PushModule/AnalyticsModule.
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.auth)
    implementation(libs.firebase.messaging)
    implementation(libs.firebase.analytics)
    // Remote Config drives the force-update gate: the server declares the minimum
    // supported versionCode; a build below it is blocked at launch. Default FirebaseApp
    // auto-inits from the per-flavor firebase.xml (same manual init as auth/messaging);
    // an env with no key set keeps min=0, so the gate fails open there.
    // See sg.mesha.goatos.update.RemoteConfigUpdateGate.
    implementation(libs.firebase.config)
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services.auth)
    implementation(libs.googleid)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)

    // In-app LIVE camera video capture for proof recording (MOB-002 anti-fraud rule: camera-
    // only, no gallery/file picker). camera-video's Recorder + camera-view's PreviewView back
    // the `video_proof` capture screen; no other module needs CameraX, so it stays :app-only.
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.video)
    implementation(libs.androidx.camera.view)

    // Virtual-time coroutine testing (runTest/advanceTimeBy) for the offline-banner debounce.
    testImplementation(libs.kotlinx.coroutines.test)
}

baselineProfile {
    dexLayoutOptimization = true
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
