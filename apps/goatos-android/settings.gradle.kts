pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "goatos-android"

// --- app (thin: Application, DI, nav host, theme, boot) ---
include(":app")
include(":benchmark")

// --- core (feature-* depend only on core-*; core-model/core-common are Android-free) ---
include(":core:core-model")
include(":core:core-common")
include(":core:core-designsystem")
include(":core:core-ui")
include(":core:core-network")
include(":core:core-media")
include(":core:core-datastore")
include(":core:core-data")
include(":core:core-database")
include(":core:core-analytics")
include(":core:core-notifications")
include(":core:core-permissions")
include(":core:core-testing")

// --- feature (feature-* -> core-* only, never feature -> feature) ---
include(":feature:feature-auth")
include(":feature:feature-calendar")
include(":feature:feature-clock")
include(":feature:feature-counts")
include(":feature:feature-feed")
include(":feature:feature-health")
include(":feature:feature-pccare")
include(":feature:feature-sheds")
include(":feature:feature-scan")
include(":feature:feature-submit")
include(":feature:feature-record")
include(":feature:feature-profile")
include(":feature:feature-timetable")
include(":feature:feature-leadership-tasks")
include(":feature:feature-toxin")
include(":feature:feature-vendors")
include(":feature:feature-vaccination")
include(":feature:feature-verify")
include(":feature:feature-weighing")

// --- device (vendor SDKs live ONLY here, behind ports; each ships a fake) ---
include(":device:device-rfid")
include(":device:device-camera")
include(":device:device-feedback")
