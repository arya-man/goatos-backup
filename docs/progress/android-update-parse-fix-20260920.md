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
- First mobile distribution attempt refused to start before build/upload: Remote Config preflight returned HTTP 403 because Firebase REST calls lacked `X-Goog-User-Project`.
- Verified Remote Config read succeeds with `X-Goog-User-Project: goatos-stg`; patched the mobile distribution script to send that header for Remote Config and FCM REST calls.
- Mobile distribution completed from SHA `4e699a6b14af`: `MOBILE_DISTRIBUTED 4e699a6b14af 1.0.34 84`.
- Firebase App Distribution release created for `sg.mesha.goatos`, release id `4b7nogs6sb67g`.
- `https://mesha.sg/app.apk` now redirects to the GCS object with content type `application/vnd.android.package-archive`, filename `Mesha-1.0.34.apk`, and stored length `90577923`.
- Built APK SHA-256 and live `mesha.sg/app.apk` SHA-256 both equal `2700ab54acf81f5f1a5d3da6112fb9b9259bde8797ce77386d0897b2a7bfebc2`.
- Live APK metadata: package `sg.mesha.goatos`, versionName `1.0.34`, versionCode `84`; `unzip -t` passed.
- Firebase Remote Config readback: `min_supported_version_code=84`, `update_url=https://mesha.sg/app.apk`.
- Play upload response: versionCode `84`, sha256 `a2c7577bfa5ccb69ebf9c271453e19782bb3a312da093e982c4c2eef14aa9997`; internal track release file contains `GoatOS 84` completed.
- Force-update recheck push sent: `projects/goatos-stg/messages/4033364145921682691`.
- Installed the published APK on connected phone `F5625U031150` via ADB; package now reports versionCode `84`, versionName `1.0.34`.
- Launched the phone app after install and captured `/tmp/goatos-after-deploy-installed.png`: app is no longer on the update-required installer screen; it shows the separate no-network screen.

## Pending

- If green, commit the Android fix in the clean worktree.
- Verify in-app network/download path on connected phone after device connectivity is restored.

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
- Mobile distribution:
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./tools/deploy/stg-mobile-distribution.sh`
- Post-deploy verification:
  - `curl -fsSL https://mesha.sg/app.apk`
  - `shasum -a 256` local build and live APK
  - `apkanalyzer manifest application-id/version-name/version-code`
  - `unzip -t`
  - Firebase Remote Config REST readback
  - `adb -s F5625U031150 install -r .local/verify-mesha-sg-app.apk`
  - `adb -s F5625U031150 shell dumpsys package sg.mesha.goatos`

## Known Failures / Blockers

- Exact installer parse dialog could not be reproduced from the app before deployment because the connected phone cannot resolve the API or APK host names.
- Post-deploy in-app download verification remains blocked by the same device network issue: `ping 8.8.8.8` reports network unreachable and `ping mesha.sg` reports unknown host.

## Before / After Metrics

- Before: updater saved whatever response body arrived as `goatos-update.apk` and opened Package Installer without validating APK bytes.
- After: updater only replaces the cached APK after a full validated APK-shaped download; bad bodies stay as `.part` and are deleted.

## Judge Status

- Focused Android updater tests: green.

## Current SHA

- Base SHA: `ec88a3f256d25c832ab0808e8597e88c6467bc55`
- Worktree: `/Users/raviteja/mesha/goatos-android-update-guard`

## Deployment State

- Mobile distribution complete for `sg.mesha.goatos` 1.0.34 (84).
- Firebase App Distribution: uploaded.
- Google Play Internal Testing: updated to versionCode 84 by script.
- Stable operator URL: `https://mesha.sg/app.apk` updated and verified.
- Firebase Remote Config force-update floor: updated and verified.
- Force-update recheck FCM: sent.
- No backend/admin-web STG deploy started.
