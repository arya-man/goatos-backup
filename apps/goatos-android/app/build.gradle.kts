import com.android.build.api.variant.HasHostTestsBuilder
import com.android.build.api.variant.HostTestBuilder
import com.google.firebase.appdistribution.gradle.firebaseAppDistribution
import org.gradle.api.tasks.testing.Test
import java.io.ByteArrayOutputStream

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

val needsFirebaseSourceMetadata = gradle.startParameter.taskNames.any {
    it.contains("StgRelease", ignoreCase = true) ||
        it.contains("appDistributionUpload", ignoreCase = true) ||
        it.contains("validateFirebaseDistributionSource", ignoreCase = true)
}

// Configuration-cache safe git access.
//
// A bare ProcessBuilder here runs during the CONFIGURATION phase, which Gradle
// rejects outright when org.gradle.configuration-cache=true (gradle.properties
// sets it): "Starting an external process ... during configuration time is
// unsupported". That made :benchmark:compileDevNonMinifiedBenchmarkKotlin fail
// 15/15 runs. providers.exec(...) is the sanctioned escape hatch: Gradle owns the
// execution, records it as a build input, and re-obtains it when reusing a cached
// entry, so the provenance values stay correct instead of being frozen into the
// cache. Guarded by tools/ci/check-gradle-config-cache.sh, which runs a real
// configuration-cache build rather than grepping for ProcessBuilder.
fun gitOutput(vararg args: String): String {
    if (!needsFirebaseSourceMetadata) return ""
    return runCatching {
        providers.exec {
            commandLine("git", *args)
            workingDir = rootProject.projectDir
            isIgnoreExitValue = true
        }.standardOutput.asText.get().trim()
    }.getOrDefault("")
}

fun quotedBuildConfig(value: String): String =
    "\"${value.replace("\\", "\\\\").replace("\"", "\\\"")}\""

val sourceCommit: String =
    System.getenv("GITHUB_SHA")?.takeIf { it.isNotBlank() }
        ?: System.getenv("COMMIT_SHA")?.takeIf { it.isNotBlank() }
        ?: gitOutput("rev-parse", "HEAD").ifBlank { "unknown" }
val shortSourceCommit = sourceCommit.take(12)
val sourceTag: String =
    System.getenv("GITHUB_REF_NAME")
        ?.takeIf { System.getenv("GITHUB_REF_TYPE") == "tag" && it.isNotBlank() }
        ?: gitOutput("describe", "--tags", "--exact-match", "HEAD")
val sourceBranch: String =
    System.getenv("GITHUB_REF_NAME")
        ?.takeIf { it.isNotBlank() }
        ?: gitOutput("branch", "--show-current")
val sourceDirty = gitOutput("status", "--porcelain").isNotBlank()
val sourceLabel = listOfNotNull(
    sourceTag.takeIf { it.isNotBlank() }?.let { "tag=$it" },
    "commit=$shortSourceCommit",
    sourceBranch.takeIf { it.isNotBlank() }?.let { "branch=$it" },
    if (sourceDirty) "dirty=true" else null,
).joinToString(" ")
val releaseVersionCode = (
    project.findProperty("goatosVersionCode") as String?
        ?: System.getenv("GOATOS_ANDROID_VERSION_CODE")
    )
    ?.takeIf { it.isNotBlank() }
    ?.toInt()
    ?: 40
val releaseVersionName = (
    project.findProperty("goatosVersionName") as String?
        ?: System.getenv("GOATOS_ANDROID_VERSION_NAME")
    )
    ?.takeIf { it.isNotBlank() }
    ?: "0.1.39"

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
        versionCode = releaseVersionCode
        versionName = releaseVersionName
        multiDexKeepProguard = file("multidex-startup-rules.pro")

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
        buildConfigField("String", "SOURCE_COMMIT", quotedBuildConfig(sourceCommit))
        buildConfigField("String", "SOURCE_TAG", quotedBuildConfig(sourceTag))
        buildConfigField("String", "SOURCE_BRANCH", quotedBuildConfig(sourceBranch))
        buildConfigField("String", "SOURCE_LABEL", quotedBuildConfig(sourceLabel))
        buildConfigField("boolean", "SOURCE_DIRTY", sourceDirty.toString())
        resValue("string", "goatos_source_commit", sourceCommit)
        resValue("string", "goatos_source_tag", sourceTag.ifBlank { "untagged" })
        resValue("string", "goatos_source_label", sourceLabel)
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
            // Test affordance ONLY. A tester holds a handful of physical RFID tags but has to
            // exercise many sheds, so the same tag is re-read in every one of them. Weighing
            // scopes its duplicate rule per shed on purpose, so those reads are all accepted --
            // and downstream they look like ONE animal weighed repeatedly, which is what fed
            // growth a 15 kg and an 11 kg reading of "the same goat" minutes apart. Namespacing
            // the scan per shed in dev builds makes 5 tags behave like 5 distinct animals in
            // each shed. It is a FLAVOUR field, so stg/prod cannot compile it in.
            buildConfigField("boolean", "SCAN_SCOPE_PREFIX", "true")
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
            buildConfigField("boolean", "SCAN_SCOPE_PREFIX", "false")
            applicationIdSuffix = ".stg"
            versionNameSuffix = "-stg"
            signingConfig = signingConfigs.getByName("stgRelease")
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
                System.getenv("GOOGLE_APPLICATION_CREDENTIALS")
                    ?.takeIf { it.isNotBlank() }
                    ?.let { serviceCredentialsFile = it }
                (project.findProperty("fadTesters") as String?)?.let { testers = it }
                releaseNotes = (project.findProperty("fadReleaseNotes") as String?)
                    ?: "Goat OS (Mesha) stg release build\n$sourceLabel"
            }
        }
        create("prod") {
            dimension = "env"
            signingConfig = signingConfigs.getByName("stgRelease")
            buildConfigField("boolean", "SCAN_SCOPE_PREFIX", "false")
            buildConfigField("String", "API_BASE_URL", "\"https://api.goatos.mesha.sg/\"")
            buildConfigField("String", "AUTH_ACTION_CONTINUE_URL", "\"https://dashboard.mesha.sg/login\"")
            // Reuses the existing goatos-stg Firebase/GCP project with the production package.
            buildConfigField(
                "boolean",
                "TELEMETRY_ENABLED",
                (project.findProperty("goatosTelemetryEnabled") as String?) ?: "false",
            )
            buildConfigField("String", "OTLP_ENDPOINT", "\"\"")
            firebaseAppDistribution {
                appId = (project.findProperty("goatosProdFirebaseAppId") as String?)
                    ?: System.getenv("FIREBASE_APP_ID")
                    ?: "1:514832198871:android:2b3a80736ff2e8d9f19492"
                artifactType = "APK"
                groups = (project.findProperty("fadGroups") as String?) ?: "goatos-testers"
                System.getenv("GOOGLE_APPLICATION_CREDENTIALS")
                    ?.takeIf { it.isNotBlank() }
                    ?.let { serviceCredentialsFile = it }
                (project.findProperty("fadTesters") as String?)?.let { testers = it }
                releaseNotes = (project.findProperty("fadReleaseNotes") as String?)
                    ?: "GoatOS (Mesha) release build\n$sourceLabel"
            }
        }
    }

    // Robolectric unit tests that drive real androidx components need the merged unit-test
    // resources on the classpath (`WorkManager.initialize` reads
    // `R.bool.workmanager_test_configuration`) — same reason :core:core-data and
    // :core:core-database already enable this.
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
        }
    }

    buildTypes {
        debug {
            manifestPlaceholders["appLabel"] = "Mesha Debug"
        }
        release {
            isMinifyEnabled = false
            manifestPlaceholders["appLabel"] = "Mesha"
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")

            // Firebase Performance Monitoring instruments bytecode via AGP's Instrumentation
            // API (com.google.firebase.perf's FirebasePerfExtension, registered as a DSL
            // extension on this buildType/each productFlavor), and that same transform
            // (`transform<Variant>UnitTestClassesWithAsm`) also runs over unit-test compile
            // output because the unitTest host-test component shares this buildType. Its ASM
            // ClassWriter uses COMPUTE_FRAMES and, for the deeply-nested synthetic classes
            // Kotlin generates for chained Flow operators in test fakes (e.g.
            // FakeScanExecutionRepository$observeScanRosterStatusCounts$$inlined$map$1), it
            // fails to resolve a common superclass and silently drops/corrupts the output class
            // file — present in compileXxxUnitTestKotlin output, absent from the transformed
            // test classes dir the test task actually runs against. That produces
            // java.lang.NoClassDefFoundError at runtime for a class that plainly compiled,
            // across every ViewModel test whose fakes chain Flow.map/groupingBy (Scan, Submit,
            // Counts, Record, Coverage banner, Rfid, SyncStatus, VerifyDetail — the full
            // `testStgReleaseUnitTest` failure set). This is a build-tooling defect, not a
            // production bug or a wrong test. Skip the transform ONLY for the invocation
            // actually running a unit-test task (checked once at configuration time against
            // this build's requested tasks), so `assembleRelease`/`bundleRelease` keep full
            // network-call instrumentation and only `test*UnitTest` runs are affected.
            val runningUnitTests = gradle.startParameter.taskNames.any {
                it.contains("UnitTest", ignoreCase = true)
            }
            extensions.configure<com.google.firebase.perf.plugin.FirebasePerfExtension> {
                setInstrumentationEnabled(!runningUnitTests)
            }
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
        resValues = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

}

dependencies {
    implementation(project(":core:core-media"))
    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.ui)
    implementation(libs.androidx.media3.transformer)
    implementation(libs.androidx.media3.effect)
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
    // areNotificationsEnabled(): the OS's own answer to "will this phone show what we send it",
    // reported to the backend on device register + heartbeat (AppModule/PushModule wiring) so a
    // push-muted device is never counted as reached.
    implementation(project(":core:core-permissions"))
    implementation(project(":core:core-analytics"))

    implementation(project(":feature:feature-calendar"))
    implementation(project(":feature:feature-clock"))
    implementation(project(":feature:feature-counts"))
    implementation(project(":feature:feature-feed"))
    implementation(project(":feature:feature-health"))
    implementation(project(":feature:feature-pccare"))
    implementation(project(":feature:feature-sheds"))
    implementation(project(":feature:feature-scan"))
    implementation(project(":feature:feature-submit"))
    implementation(project(":feature:feature-record"))
    implementation(project(":feature:feature-profile"))
    implementation(project(":feature:feature-timetable"))
    implementation(project(":feature:feature-toxin"))
    implementation(project(":feature:feature-vaccination"))
    implementation(project(":feature:feature-verify"))
    implementation(project(":feature:feature-weighing"))

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
    debugImplementation(libs.androidx.compose.material.icons.extended)
    debugImplementation(libs.showkase)
    debugImplementation(libs.showkase.annotation)

    // In-app LIVE camera video capture for proof recording. Shed-level proof may also use the
    // Android gallery picker when the backend SOP explicitly allows it; CameraX still backs the
    // `video_proof` recorder screen, and no other module needs CameraX, so it stays :app-only.
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.video)
    implementation(libs.androidx.camera.view)

    // Virtual-time coroutine testing (runTest/advanceTimeBy) for the offline-banner debounce.
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
    // Flow state-emission testing (turbine: deterministic collection of Flow emissions
    // in test context, no manual launch/collect needed). Catches state-sequence bugs:
    // flashes, wedges, yanks, stale-scope issues (item: state-sequence tests).
    testImplementation(libs.turbine)
    // Retrofit and OkHttp for creating mock HttpException with proper error bodies in tests
    testImplementation(libs.retrofit)
    testImplementation(libs.okhttp)
}

configurations.matching { configuration ->
    configuration.name in setOf("kspDevDebug", "kspStgDebug", "kspProdDebug")
}.configureEach {
    project.dependencies.add(name, libs.showkase.processor)
}

ksp {
    arg("skipPrivatePreviews", "true")
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

val validateStgReleaseInputs = tasks.register("validateStgReleaseInputs") {
    group = "verification"
    description = "Fails fast when the stg release signing inputs have not been restored."

    // Configuration-cache safe: a task ACTION may not hold a reference to the
    // Gradle script/Project. `project.findProperty(...)` and `file(...)` inside
    // doLast did exactly that, and the cache refused to serialise the task
    // ("cannot serialize Gradle script object references"). Resolve every
    // Project-dependent value HERE, at configuration time, and let doLast close
    // over plain data. Guarded by tools/ci/check-gradle-config-cache.sh.
    val required = listOf(
        "GOATOS_ANDROID_STG_KEYSTORE",
        "GOATOS_ANDROID_STG_KEYSTORE_PASSWORD",
        "GOATOS_ANDROID_STG_KEY_ALIAS",
        "GOATOS_ANDROID_STG_KEY_PASSWORD",
    )
    val resolvedSigning: Map<String, String?> = required.associateWith { name ->
        providers.gradleProperty(name).orElse(providers.environmentVariable(name)).orNull
    }
    val keystoreValue = resolvedSigning["GOATOS_ANDROID_STG_KEYSTORE"]
    val keystoreFile = keystoreValue
        ?.takeIf { it.isNotBlank() }
        ?.let { rootProject.layout.projectDirectory.file(it).asFile }

    doLast {
        val missing = required.filter { resolvedSigning[it].isNullOrBlank() }
        val keystore = keystoreValue
        val missingKeystoreFile = keystoreFile != null && !keystoreFile.isFile

        if (missing.isNotEmpty() || missingKeystoreFile) {
            val missingText = if (missing.isEmpty()) {
                ""
            } else {
                "Missing: ${missing.joinToString(", ")}\n"
            }
            val keystoreText = if (missingKeystoreFile) {
                "Keystore file does not exist: $keystore\n"
            } else {
                ""
            }
            throw GradleException(
                "${missingText}${keystoreText}" +
                    "Restore stg Android signing from ../../docs/mobile/stg-signed-release.md. " +
                    "Secrets live in GCP Secret Manager project goatos-stg; do not invent or paste " +
                    "keystore/password values in chat. For Firebase App Distribution auth, use " +
                    "GOOGLE_APPLICATION_CREDENTIALS or firebase login.",
            )
        }
    }
}

val validateFirebaseDistributionSource = tasks.register("validateFirebaseDistributionSource") {
    group = "verification"
    description = "Fails App Distribution upload when the APK cannot be traced to a commit/tag."

    doLast {
        if (sourceCommit == "unknown" || sourceCommit.length < 12) {
            throw GradleException("Firebase App Distribution requires a real git commit SHA.")
        }
        val allowDirty = (project.findProperty("allowDirtyFirebaseDistribution") as String?)
            ?.equals("true", ignoreCase = true) == true
        if (sourceDirty && !allowDirty) {
            throw GradleException(
                "Refusing Firebase App Distribution upload from a dirty worktree. " +
                    "Commit the APK source first, or pass -PallowDirtyFirebaseDistribution=true for an explicit throwaway build. " +
                    "Source would have been: $sourceLabel",
            )
        }
    }
}

tasks.matching {
    it.name == "assembleStgRelease" || it.name == "appDistributionUploadStgRelease"
}.configureEach {
    dependsOn(validateStgReleaseInputs)
}

tasks.matching {
    it.name == "appDistributionUploadStgRelease"
}.configureEach {
    dependsOn(validateFirebaseDistributionSource)
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

tasks.withType<Test>().configureEach {
    if (name == "testStgReleaseUnitTest") {
        // Paparazzi goldens are recorded/verified for devDebug only. Keep stgRelease unit tests
        // enabled for Firebase/release wiring, but do not run screenshot classes there because
        // Paparazzi looks for variant-specific snapshot resources and fails before comparing UI.
        exclude("sg/mesha/goatos/ui/*ScreenshotTest*")
    }
}

tasks.matching {
    it.name.startsWith("transform") && it.name.endsWith("UnitTestClassesWithAsm")
}.configureEach {
    outputs.cacheIf { false }
}
