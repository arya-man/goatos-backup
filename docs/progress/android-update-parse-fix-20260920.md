# Android Update Parse Fix - 2026-09-20

## Scope

- Fix the production-facing GoatOS Android force-update sideload path so corrupt, partial, or non-APK downloads are rejected before Android Package Installer opens.
- Publish only the mobile distribution if local proof passes.
- Do not deploy backend/admin-web for this fix unless evidence shows the live APK mirror or Remote Config is broken.

## Done

- Verified live `https://mesha.sg/app.apk` from the laptop: HTTP 200, APK content type, about 87 MB, and ZIP/APK structure passes.
- Verified connected production phone `F5625U031150` has `sg.mesha.goatos` versionCode 56 / versionName 1.0.6.
- Attempted on-phone app reproduction: app currently shows `Couldn't reach the server. Check your connection and try again.`
- Captured logcat after Retry: API calls fail with `UnknownHostException`; device DNS also cannot resolve `api.goatos.mesha.sg` or `mesha.sg`.
- Added atomic `.part` APK download, HTTP 2xx check, content-length check, APK ZIP validation, and stale partial cleanup.
- Added focused updater validation tests.
- Focused Android unit gate passed in clean worktree:
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.update.SideloadUpdateInstallerTest' --tests 'sg.mesha.goatos.update.RemoteConfigUpdateGateTest'`

## Pending

- If green, commit the Android fix in the clean worktree.
- Run mobile distribution through `tools/deploy/stg-mobile-distribution.sh`.
- Verify published `https://mesha.sg/app.apk` package/version/hash after distribution.
- Verify on connected phone after distribution if device DNS/network recovers; otherwise document the phone-network blocker.

## Tests / E2E Performed

- Laptop live APK validation:
  - `curl -L --fail https://mesha.sg/app.apk`
  - `file /tmp/mesha-live-app.apk`
  - `unzip -t /tmp/mesha-live-app.apk`
- Phone replication attempt:
  - `adb -s F5625U031150 shell monkey -p sg.mesha.goatos 1`
  - screenshot `/tmp/goatos-before-update.png`
  - Retry tap + logcat
  - `ping api.goatos.mesha.sg` and `ping mesha.sg` both failed with unknown host on device.

## Known Failures / Blockers

- Exact installer parse dialog could not be reproduced from the app before deployment because the connected phone cannot resolve the API or APK host names.

## Before / After Metrics

- Before: updater saved whatever response body arrived as `goatos-update.apk` and opened Package Installer without validating APK bytes.
- After: updater only replaces the cached APK after a full validated APK-shaped download; bad bodies stay as `.part` and are deleted.

## Judge Status

- Focused Android updater tests: green.

## Current SHA

- Base SHA: `ec88a3f256d25c832ab0808e8597e88c6467bc55`
- Worktree: `/Users/raviteja/mesha/goatos-android-update-guard`

## Deployment State

- No mobile distribution started yet.
- No backend/admin-web STG deploy started.
