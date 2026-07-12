plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
}

// Pure persistence layer (TRD §3 mirrors core-datastore's independence): the local
// outbox Room schema only. No project(":core:...") deps on purpose — SyncEngine /
// SyncRepository / OutboxStore (the actual sync business logic + the port other
// modules consume) live in :core:core-data, which depends on this module. Keeping
// this module dependency-free maximizes reuse and keeps the outbox schema testable
// in isolation.
android {
    namespace = "sg.mesha.goatos.core.database"
    compileSdk = 36
    defaultConfig {
        minSdk = 29
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    // Robolectric-backed migration test (OutboxDatabaseMigrationTest) needs a real
    // (shadowed) Context + android.database.sqlite under a plain testDebugUnitTest run.
    // isIncludeAndroidResources also makes Robolectric read the unit-test merged assets,
    // so the exported Room schema JSON (room.schemaLocation below) resolves for
    // MigrationTestHelper without a device/emulator.
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
        }
    }
    sourceSets {
        getByName("test").assets.srcDir("$projectDir/schemas")
        getByName("androidTest").assets.srcDir("$projectDir/schemas")
    }
}

// Room exports one JSON schema per @Database version to $projectDir/schemas — committed,
// reviewed as diffs, and the golden schema MigrationTestHelper validates the outbox's
// OUTBOX_MIGRATION_1_2 / 2_3 against. The outbox holds not-yet-synced operator writes,
// so a silently-wrong migration here loses data — validated migrations are mandatory.
ksp {
    arg("room.schemaLocation", "$projectDir/schemas")
}

dependencies {
    implementation(libs.kotlinx.coroutines.core)
    api(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    // Robolectric + androidx.test.core: real shadowed Context for the Room builder / helper.
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
    // MigrationTestHelper — runs OUTBOX_MIGRATION_* against the committed golden schema JSON
    // and validates the migrated schema matches the @Entity definitions.
    testImplementation(libs.androidx.room.testing)
}
