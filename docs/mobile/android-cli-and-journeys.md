# Android CLI and Journeys

This is Goat OS guidance for Google's Android CLI, Android skills, and
agent-run Journeys. It is additive to the existing Gradle, lint, unit-test,
Paparazzi, and phone-QA proof stack. It does not replace those gates.

## What Is Installed

Run the repo bootstrap before Android developer work:

```bash
bash tools/dev/ensure-android-cli.sh
```

The helper is idempotent. On a fresh developer machine it installs the
user-local Android CLI, runs `android update`, runs `android init`, and runs
`android skills add --all`. On a machine that already has Android CLI, it only
fills missing base skills or the broader Android skill set.

Current local verification:

```text
android version: 1.0.15985488
sdk: /Users/ravi/Library/Android/sdk
```

`android init` installs the base `android-cli` skill for detected agents,
including Codex and Claude. `android skills add --all` installs the broader
official skill set such as `testing-setup`, `camerax`, `adaptive`,
`edge-to-edge`, `navigation-3`, `android-profiler`, `r8-analyzer`,
`agp-9-upgrade`, `android-intent-security`, `appfunctions`, and related Android
workflow skills.

Installed official Android skills that matter most for Goat OS Compose work:

| Skill | Use in Goat OS |
|---|---|
| `adaptive` | Large phone/tablet/foldable layout work, navigation rail decisions, multi-pane list-detail/supporting-pane work. |
| `edge-to-edge` | System bar, IME inset, bottom bar, status bar, and navigation bar overlap fixes. |
| `navigation-3` | Research or migration planning for Compose-first Navigation 3 patterns. Goat OS does not migrate to Nav3 without a deliberate architecture decision. |
| `styles` | Researching Jetpack Compose Styles API adoption. Treat as exploratory while Styles is alpha. |
| `migrate-xml-views-to-jetpack-compose` | Legacy view-to-Compose migrations. Mostly reference-only for Goat OS because the app is already Compose. |
| `testing-setup` | Android unit/UI/screenshot/e2e test strategy checks. |
| `android-profiler` | Trace, memory, jank, and performance investigations. |
| `camerax` | Camera/proof-capture work. |
| `android-intent-security` | Manifest/component/Intent review. |
| `agp-9-upgrade` | AGP upgrade work only. Do not invoke for routine feature work. |
| `r8-analyzer` | Release size/keep-rule investigations. |

## What Android CLI Provides

Use Android CLI as the shared agent/device/documentation surface:

```bash
android info
android sdk list <pattern>
android sdk install <package[@version]>
android emulator list
android emulator start <avd-name>
android run --device=<serial> --apks=<apk-path>
android install --device=<serial> --apks=<apk-path>
android layout --device=<serial> --pretty
android layout --device=<serial> --diff
android screen capture --device=<serial> --output=screen.png
android screen capture --device=<serial> --annotate --output=screen.png
android screen resolve --screenshot=screen.png --string="input tap #5"
android docs search "query"
android docs fetch kb://android/...
android skills list --long
android skills find performance
android skills add --skill=<skill-name>
```

For Goat OS, the useful pieces are:

- environment and SDK diagnosis before a phone run;
- emulator creation/start/stop/list commands for agent-owned test devices;
- APK install/run commands as a higher-level wrapper over raw `adb install`;
- `android layout` for compact UI-tree inspection before using screenshots;
- `android screen capture` and `screen resolve` for visual proof and tap
  coordinates when the layout tree is insufficient;
- `android docs search/fetch` for current official Android documentation;
- Android skills for specialized tasks where current Android best practice
  matters.

## Journeys

Journeys are not a separate binary in the current CLI. They are agent-run test
specifications backed by the Android CLI skills and device tools.

The installed `android-cli` skill defines a Journey as XML with a name,
description, and ordered `<action>` elements:

```xml
<journey name="Vaccination proof upload">
  <description>
    Operator opens a vaccination shed, scans an RFID, records proof, and submits.
  </description>
  <actions>
    <action>Verify that the app shows the operator vaccination worklist</action>
    <action>Tap the first open shed task</action>
    <action>Scan or enter the test RFID</action>
    <action>Verify that the captured goat appears in the proof list</action>
    <action>Tap the proof capture action</action>
    <action>Verify that the shed remains open for final submit after proof upload</action>
  </actions>
</journey>
```

An agent evaluates each action in order. Interaction actions should do only the
specified interaction and basic crash/unexpected-behavior checks. Actions that
begin with "check" or "verify" are assertions against the current screen. A
failed action stops the Journey and should be reported as test evidence, not
hand-waved into a pass.

Use Journeys for real-device functional proof where Paparazzi cannot see the
runtime system:

- login/dev-token bootstrap;
- role-specific navigation and backend-driven chrome;
- RFID scan path;
- camera/proof capture;
- Room/outbox state after upload;
- reconnect and sync behavior;
- `adb reverse` and local-backend wiring;
- operator/director/CEO workflow differences.

Do not use Journeys as the only proof for UI rendering. Paparazzi remains the
pixel/golden gate. Journeys answer "can a real device complete the workflow?"
Paparazzi answers "did the intended screen render correctly?"

## Compose Agent Loop

For Jetpack Compose work, use the loop from the Android CLI + Claude Code
workflow research:

1. Start with one bounded feature or bug and explicit acceptance criteria.
2. Let the agent inspect the existing project and use local architecture.
3. Run the cheapest checks first: Gradle compile, unit tests, lint/static checks.
4. Build the APK before starting or touching an emulator.
5. Deploy only a built APK.
6. Inspect the real UI with `android layout --pretty` and screenshots.
7. Run the relevant Journey for the user flow.
8. Review the diff and evidence before accepting completion.

Apply this Goat OS translation:

```text
Compose source change
  -> targeted Gradle compile/unit/lint
  -> Paparazzi when UI/goldens are affected
  -> Android CLI deploy/layout/screenshot for phone proof
  -> Journey for critical runtime workflows
```

Do not let the agent skip straight from "Kotlin looks right" to "done." The
valuable part of Android CLI is the movement from source code to build output to
visible app behavior.

## Compose Skill Policy

Official Android skills are useful because current Compose guidance changes
quickly and the model may otherwise fall back to stale patterns.

Use these skills deliberately:

- `edge-to-edge`: when Goat OS UI overlaps system bars, keyboard, bottom nav, or
  status bar content.
- `adaptive`: when a screen needs phone/tablet/foldable behavior, navigation
  rail, or multi-pane layout decisions.
- `testing-setup`: when adding a new Android test layer or deciding between
  unit, Compose UI, Paparazzi, device, and Journey proof.
- `camerax`: when touching proof capture, camera lifecycle, recording, or media
  capture behavior.
- `android-profiler`: when debugging jank, startup, memory, traces, or runtime
  performance.
- `navigation-3`: only for research/planning unless Goat OS explicitly decides
  to migrate navigation. Current app navigation remains the existing Compose nav
  architecture.
- `styles`: research only unless Goat OS intentionally adopts Compose Styles.
  The official Styles docs currently point at alpha Compose dependencies, so do
  not upgrade Compose just to satisfy the skill.

Community Compose skills exist, including Compose-performance and Compose-source
receipt skills. Do not install or commit third-party skills into Goat OS by
default. Evaluate them separately before adoption, because official Android
skills already cover the current workflow and are installed by the repo helper.

## Goat OS Operating Rules

1. Keep existing Gradle and Paparazzi gates as the authoritative CI checks.
2. Use Android CLI/Journeys for phone-QA evidence and agent-driven debugging.
3. Always preserve the Goat OS Android profile rule: target the visible Android
   user/profile for install, clear, launch, and screenshots.
4. For phone QA, keep using the throwaway DB and non-default host API port rules
   in `AGENTS.md` and `docs/runbooks/phone-qa-throwaway-rbac.md`.
5. A Journey pass must include the device serial, backend target chain, Journey
   file/name, per-action result, and any screenshot/layout artifacts.
6. A Journey failure is useful signal. Report the first failed action and the
   observed screen/state instead of retrying until it passes.
7. Prefer `android layout --diff` between steps to keep evidence compact; use
   annotated screenshots when layout is missing WebView/animation/visual detail.

## Current Limitations and Cautions

- The current Android CLI exposes `run`, `install`, `layout`, `screen`, `sdk`,
  `skills`, `docs`, `emulator`, `describe`, and `studio` commands. It does not
  expose a standalone `android journey` subcommand in this installed build.
- Google's docs and the installed skill describe Journeys as agent-created and
  agent-run using Android CLI tools and skills.
- Android CLI collects basic usage metrics by default. Google's docs say command
  invocations, subcommands, option names, selected predefined values, and
  anonymized stack traces may be collected, while command responses and custom
  user inputs are not. Use `--no-metrics` for invocations where that matters.
- The public Android skills repository currently has open issues against the
  `android-cli` skill docs, including Journey/example-command documentation
  bugs. Treat the local CLI help and current official docs as the source of
  truth when examples disagree.
- Community feedback on Reddit/Hacker News is skeptical of marketing speed
  claims and more interested in verification. For Goat OS, use this tooling for
  repeatable phone proof, not as a replacement for disciplined tests.

## Sources Checked

- Android CLI overview:
  https://developer.android.com/tools/agents/android-cli
- Android CLI Journeys:
  https://developer.android.com/tools/agents/android-cli/journeys
- Android CLI release notes:
  https://developer.android.com/tools/agents/android-cli/release-notes
- Android skills overview:
  https://developer.android.com/tools/agents/android-skills
- Android skills repository:
  https://github.com/android/skills
- Android skills issues:
  https://github.com/android/skills/issues
- Android skills browse:
  https://developer.android.com/tools/agents/android-skills/browse
- Jetpack Compose Styles docs:
  https://developer.android.com/develop/ui/compose/styles
- Jetpack Navigation 3 docs:
  https://developer.android.com/guide/navigation/navigation-3
- Adaptive Compose skill:
  https://github.com/android/skills/blob/main/jetpack-compose/adaptive/SKILL.md
- Edge-to-edge skill:
  https://github.com/android/skills/blob/main/system/edge-to-edge/SKILL.md
- Android Developers Blog, Android CLI 1.0:
  https://android-developers.googleblog.com/2026/05/android-cli-stable-1-0-agent-development.html
- Android developer productivity update:
  https://android-developers.googleblog.com/2026/06/android-developer-productivity-updates.html
- LinkedIn article, "Claude Code Meets Android CLI: Build, Run, and Test":
  https://www.linkedin.com/pulse/claude-code-meets-android-cli-build-run-test-mohamad-abuzaid-rjavf/
- Reddit discussion:
  https://www.reddit.com/r/androiddev/comments/1snee1b/android_cli_build_android_apps_3x_faster_using/
- Hacker News discussion:
  https://news.ycombinator.com/item?id=47797665
