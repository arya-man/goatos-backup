# ADR: Goat OS mobile app id, namespace & flavors (env-only; roles are runtime)

Status: accepted (maintainer decision, this session). Pairs with
[`mobile-native-kotlin.md`](mobile-native-kotlin.md) (client tech = native Kotlin).

## Context

The Goat OS Android app is **one common, role-aware app** for the field operator
**and** leadership (Park Manager, Director, CEO/COO). Role, park/shed scope,
grants, and visible navigation come from `/app/bootstrap` at runtime — the mock
proves one app where switching the logged-in person switches the lens. RBAC is
server-authoritative.

Two questions had to be locked before any Gradle exists: (1) the application id /
package name (permanent once on Play), and (2) whether roles should be Gradle
product flavors or a runtime concern. Earlier docs had drifted to
`sg.mesha.goatos.operator` + module `apps/operator-android` — an operator-only
identity that contradicts the common-app model.

## Decision

- **App id / namespace**

  | | value |
  |---|---|
  | `namespace` (fixed; R/BuildConfig package) | `sg.mesha.goatos` |
  | `applicationId` prod | `sg.mesha.goatos` |
  | `applicationId` dev | `sg.mesha.goatos.dev` |
  | `applicationId` stg | `sg.mesha.goatos.stg` |
  | Gradle root | `apps/goatos-android/` |
  | Firebase app nickname | Goat OS / Goat OS Dev / Goat OS Stg |

- **Flavors are for ENVIRONMENT only** (`dev` / `stg` / `prod`), never for
  business persona. `namespace` stays fixed; only `applicationId` varies via
  `applicationIdSuffix`, so R/BuildConfig packages are stable and the `.dev`/`.stg`
  builds install side-by-side with a distinct label.

  ```kotlin
  android {
    namespace = "sg.mesha.goatos"
    defaultConfig { applicationId = "sg.mesha.goatos" }
    flavorDimensions += "env"
    productFlavors {
      create("dev")  { dimension = "env"; applicationIdSuffix = ".dev"
                       resValue("string","app_name","Goat OS Dev") }
      create("stg")  { dimension = "env"; applicationIdSuffix = ".stg"
                       resValue("string","app_name","Goat OS Stg") }
      create("prod") { dimension = "env"
                       resValue("string","app_name","Goat OS") }
    }
  }
  ```

- **Roles are runtime contract data, not build flavors.** No
  `operator/manager/director/ceo` flavors. `login → /app/bootstrap → role + grants
  + scope + nav`; the same app renders a different home/nav/actions per role.

## Rejected: persona flavors

Compile-time persona flavors (operator/manager/director/ceo) would mean multiple
binaries, config/logic drift between variants, N× build + review + pen-test paths,
per-persona Play listings, and — worst — would push permission decisions into the
build instead of the server, breaking server-authoritative RBAC. Hard no.

## Consequences

- One AAB serves every role; one security model; one review/pen-test path.
- Each `applicationId` is a separate Firebase app registration → per-flavor
  `google-services.json` (`sg.mesha.goatos`, `.dev`, `.stg`); see
  `docs/mobile/firebase-india-setup.md`.
- **appId is permanent on Play** — the Play Console account/signing must live under
  the **Mesha/VGoats** org (org-boundary rule), not Heva/Slice. Reverse-domain
  `sg.mesha` is the brand domain (`mesha.sg`), chosen over the cloud-org domain
  `com.vgoats` for the user-facing app identity.
- Module renamed `apps/operator-android` → `apps/goatos-android`; the RN
  `apps/operator-mobile/` scaffold remains the superseded, untouched legacy.
- If a genuinely separate future native app appears (e.g. a farmer/partner app),
  it takes its own id (`sg.mesha.goatos.<x>` or a distinct product id) — this
  decision does not pre-reserve those.
