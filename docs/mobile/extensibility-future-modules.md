# Extensibility — Future Modules & Verticals (Mobile)

Goat OS is a generic, multi-vertical OS. The first shipped mobile module is
**Preventive Care → Vaccination**. The app must leave clean room for future
modules/verticals **without a rewrite** and **without showing unbuilt modules as
live** (scope-lock rule). This mirrors the admin-web taxonomy.

## 1. Taxonomy (same words as the rest of Goat OS)

- **Vertical** = operating domain (Preventive Care, Counts, Breeding,
  Procurement, HR/People, …).
- **Module** = a concrete workflow inside a vertical (Preventive Care →
  Vaccination today; → Treatment/Deworming next; Procurement → field capture).
- **Command lens / authority** screens (Control Tower, Action Center, Config, SOP
  Library) stay **web-owned**; the phone executes/records, it does not author.

The syringe icon = Vaccination **module** only, never the Preventive Care
vertical. Do not bake "vaccination" into shell/sync/theme/bootstrap layers — keep
them generic and category/schema-driven.

## 2. Module registry (the extension point)

The app shell is module-agnostic. A module is a self-contained `feature-*` Gradle
module that registers itself through a `MobileModule` contract in `core-model`:

```kotlin
interface MobileModule {
  val id: String                       // e.g. "pc.vaccination"
  val vertical: String                 // e.g. "preventive_care"
  val navGraph: NavGraphBuilder.() -> Unit
  val routes: Map<RouteId, Route>      // backend route/action IDs → this module's screens
  val icon: IconToken
  // NO client role logic. The module declares only its id + an ID→screen map.
  // Visibility, the per-principal home route, and which actions are enabled all
  // come from bootstrap; the module never evaluates grants or role itself.
}
```

- The **app module** collects registered modules (Hilt multibinding
  `@IntoSet`) and builds the nav host + drawer, but **shows a module only if the
  backend returned its `id` in the bootstrap visible-module set** — the app does
  not run a client `isEnabled(role/grants)` predicate. It lands on the
  **backend-provided home route ID** and maps every route/action ID the backend
  sends to a screen via `routes`. Adding a vertical = adding a new `feature-*`
  module + its registration; **no shell edits, no cross-module deep imports**
  (CI-enforced boundary).
- **Backend-driven visibility**: the mobile bootstrap contract returns which
  modules/nav entries are visible for the principal (same as `/admin-web/bootstrap`).
  Unbuilt modules simply aren't returned; the drawer's "Soon" entries (mock) are
  backend-flagged placeholders, never tappable live features.

## 3. What is generic (reused by every future module)

These live in `core-*` / `device-*` and are **not** vaccination-specific:

```text
shell / nav / theme / design-system      core-designsystem, core-ui, app
auth / session / role-lens / bootstrap    core-network, feature-auth
offline sync engine + outbox + idempotency  core-data (submission/outbox generic)
forms-runner (DSL render + proof)         reused for any module's SOP/form
media capture (camera) + signed upload    device-camera, media uploader
RFID / device ports                       device-rfid + future device-* adapters
analytics / crash / push ports            core-analytics, core-notifications
i18n / time formatting (Asia/Kolkata display only) / errors  core-common
```

A new module reuses all of the above and only adds its screens + read/write
contracts. The **submission/outbox model is generic** (form_version + payload +
idempotency), so Treatment, Counts, Breeding capture flow through the same sync
engine as Vaccination.

## 4. Adding a new module — checklist

```text
[ ] Backend: publish the module's read/submit OpenAPI contracts; add to mobile
    bootstrap visibility + pinned form versions
[ ] Create feature-<module> Gradle module (depends only on core-*)
[ ] Implement MobileModule; register via Hilt @IntoSet
[ ] Screens use core-ui/design-system components (mock fidelity for visual/layout/
    interaction; the backend contract remains the source for data/state/actions)
[ ] Reuse forms-runner + sync engine + media/RFID ports; add a new device-*
    adapter only if new hardware is needed (each with a fake)
[ ] Render only backend-returned visible modules/actions (no client grant predicate); never hardcode nav
[ ] Tests: orchestration/render + sync (idempotency quartet) + Compose + a Maestro flow
[ ] No cross-feature imports (CI boundary check stays green)
```

## 5. Future surfaces (not this build, reserved)

- **iOS**: operators are Android-only; leadership uses admin-web responsive.
  If a native iOS app is ever required, the domain/`core-model` + contracts are
  UI-agnostic and reusable; a Swift/SwiftUI client would re-implement only the
  presentation layer against the same OpenAPI contracts. No backend change.
- **New hardware** (scale, ultrasound, collar, camera booth): add a `device-*`
  adapter behind a port (the backend already defines `DeviceObservationGateway`);
  the sync/submission path is unchanged.
- **New verticals** (Counts, Breeding, Procurement field): new `feature-*`
  modules on the same shell + registry.

## 6. Guardrails so extensibility stays real

- Shell/theme/sync/bootstrap must stay **module-agnostic** — no `if (vaccination)`
  branches in generic layers.
- CI **boundary check**: `feature-*` may not import another `feature-*`; vendor
  SDKs only in adapters; `core-model`/`core-common` are Android/vendor-free.
- Bootstrap-driven nav: the app renders only what the backend authorizes; adding
  a module is additive, never a shell rewrite (the mobile equivalent of the
  admin-web no-rewrite surface rule).
