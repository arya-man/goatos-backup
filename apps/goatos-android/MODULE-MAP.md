# Module map & repo-separation boundary

22 Gradle modules. Dependency rule (TRD §3, to be CI-enforced): `feature-* → core-*`
only; a feature never imports another feature; `core-model`/`core-common` are
Android-free; vendor SDKs live only in `device-*` behind ports.

```
:app                         thin — Application, boot, nav host + shell, theme, (DI graph next)
  ├─ :feature:feature-calendar   universal landing (real-ish); other features are placeholders
  └─ :core:core-data → :core:core-network → :core:core-model

core/
  core-model         (pure JVM)  nav contract: NavChrome / NavItem / NavState  ← mirrors /app/bootstrap
  core-common        (pure JVM)  DispatcherProvider, AppResult
  core-designsystem  (compose)   GoatOsTheme (dark-default, tokens from design-system.md)
  core-ui            (compose)   shared stateless components
  core-network       (android)   AppApi port + DTOs + FakeAppApi + DTO→NavState  (Retrofit/OpenAPI client next)
  core-datastore     (android)   SessionStore port (Proto DataStore next)
  core-data          (android)   BootstrapRepository (Room + sync/outbox next)
  core-analytics     (android)   AnalyticsPort + NoopAnalytics       (Firebase adapter GATED)
  core-notifications (android)   NotificationsPort + NoopNotifications (FCM adapter GATED)
  core-testing       (android)   fakes/fixtures

feature/   (feature-auth, feature-sheds, feature-scan, feature-submit,
            feature-record, feature-profile — Compose screen stubs;
            feature-calendar is the wired landing)

device/    device-rfid · device-camera · device-feedback  — Port interface + Fake each
           (Chainway RFID / CameraX / haptics adapters land behind these, vendor-isolated)
```

## Extracting to a standalone repo

Self-contained by construction:

1. Move `apps/goatos-android/` to the new repo root.
2. Backend coupling is the OpenAPI contract only. Either vendor a copy of
   `contracts/openapi/app-api.yaml` or consume the published contract, and point
   the (to-be-added) OpenAPI Kotlin generator at it — no Go/backend source is
   referenced anywhere in this tree.
3. Nothing else changes: own Gradle wrapper, own `gradle/libs.versions.toml`, no
   `project(":..")` deps that reach outside `apps/goatos-android/`.

## Not committed (gitignored)

`build/`, `.gradle/`, `local.properties`, `google-services.json`. The Gradle
wrapper jar IS committed (reproducible builds). Firebase config is gated and
machine/CI-provided.
