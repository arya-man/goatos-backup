import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
}

// Pure Kotlin/JVM — NO Android or vendor deps (TRD §3 boundary). Holds contract +
// presentation models and the nav/module-registry contract shared app-wide.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

// Match Kotlin's JVM target (build runs on JDK 21; both compile to 17 bytecode so
// the Android modules, which target 17, can consume this module).
java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

dependencies {
    implementation(libs.kotlinx.serialization.json)
    api(libs.kotlinx.collections.immutable)
}
