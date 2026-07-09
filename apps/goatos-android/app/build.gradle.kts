plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.hilt)
    alias(libs.plugins.ksp)
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
        buildConfigField("String", "DEV_BEARER_TOKEN", "\"$devToken\"")
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
        }
        create("stg") {
            dimension = "env"
            applicationIdSuffix = ".stg"
            versionNameSuffix = "-stg"
            buildConfigField("String", "API_BASE_URL", "\"https://stg.api.goatos.mesha.sg/\"")
        }
        create("prod") {
            dimension = "env"
            buildConfigField("String", "API_BASE_URL", "\"https://api.goatos.mesha.sg/\"")
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

    implementation(libs.hilt.android)
    implementation(libs.hilt.navigation.compose)
    ksp(libs.hilt.compiler)

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui.tooling.preview)
    debugImplementation(libs.androidx.compose.ui.tooling)
}
