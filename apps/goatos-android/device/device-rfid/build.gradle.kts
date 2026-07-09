plugins {
    alias(libs.plugins.android.library)
}

android {
    namespace = "sg.mesha.goatos.device.rfid"
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
    implementation(libs.kotlinx.coroutines.core)
}
