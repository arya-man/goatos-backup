# Lane 5 — Android on Firebase Test Lab virtual devices

The phone is where the farm actually works. This lane checks the Android app the
way a stockman uses it: open the app, record what you did, lose your signal, have
the app killed under you, and find your morning's work still there when the signal
comes back.

It is built from `tools/dashboard-automation/lane-checks.json` — **47 deduplicated
checks derived from 1051 Android commits since 2026-08-01**, each carrying the
commits it covers. Nothing here is a hand-written wish list; every journey traces
back to work the team actually did.

## What this lane can prove today, honestly

**2 of the 47 journeys**, and each of only half its check. Everything past the sign-in
screen needs a backend to talk to, and by Ravi's instruction **no backend may be used** —
see the next section, which is the real constraint on this lane and a decision rather
than an obstacle. Those journeys are **parked with that reason**, never reported as
covered. A green that only means "we could not try" is the exact thing this lane exists
to prevent.

| | |
|---|---:|
| Runs on a virtual device today (partially — each says which half) | 2 |
| Parked, reason on every row — 37 of them because no backend may be used | 40 |
| Physical-device only — a virtual run may never claim these | 5 |
| **Total** | **47** |

`tools/dashboard-automation/android-journeys.json` carries this per journey in
`automation.tier`, and `check-static-inventory.mjs` fails the build if a physical
journey ever picks up a test class, if something claimed as covered stops saying
which half it proves, or if parked work loses its reason.

## The data rule: no journey here connects to any real backend

**This matters more than the budget rule.**

Ravi's instruction, and it is absolute: **no write-path journey in this lane runs
against any real backend** — not the stg API, not production, not the OCI clone, not a
local one. They are catalogued here as data, with their story and their assertion, and
they are not executed. Parked is the honest outcome and it is preferred to anything that
goes near real farm data.

The naming trap this closes: the stg API serves the **same STG-backed data as
production**. "stg prod is still prod only" — the difference is a name, not the data
behind it. Neither is ever a valid target.

The app is one codebase with three flavours, and the flavour decides the backend:

| Flavour | Application id | Talks to |
|---|---|---|
| `dev` | `sg.mesha.goatos.dev` | a local backend — the only flavour this lane may use |
| `stg` | `sg.mesha.goatos.stg` | the deployed stg API — **real farm data** |
| `prod` | `sg.mesha.goatos` | production — **real farm data** |

### The guard

`run-android-journeys.mjs` refuses on **three independent reads** before it submits
anything. Any one of them refusing stops the run:

1. **The APK's flavour**, read with `aapt2 dump badging`. Only `sg.mesha.goatos.dev`
   passes. If the id cannot be read it **refuses** rather than assuming — "probably the
   dev build" is not good enough when being wrong means writing into a real farm's
   records.
2. **A scan of the APK binary** for either production-backed hostname.
3. **Any backend URL it is given**, by hostname, including subdomains. An unparseable
   URL is refused, never assumed safe.

**The guard holds no networking capability at all.** It refuses on the string, before
anything could connect — it never resolves DNS and never opens a socket, not to check a
response shape, not once. A test asserts the file imports no socket library and calls no
`fetch`, so it cannot connect even by accident. The tests prove the mechanism against
**invented hostnames** (`prod.example.invalid`); they never name a real one.

**Never the Cloud Build artifact.** Cloud Build ships a *signed release* APK to Firebase
App Distribution. Wrong artifact mechanically — instrumentation needs a debug build and a
matching test APK — and a release flavour, so it points at real data. Nothing here uses
it and nothing here triggers it.

## Where each journey can run

| | |
|---|---:|
| **Firebase Test Lab virtual devices** — needs no backend at all | 5 |
| **Parked** — needs a backend, and no backend may be used | 37 |
| **A real farm phone**, pre-release — camera, video, RFID/NFC, BLE | 5 |

### An honest correction worth recording

The offline-queue, retry, resume-after-force-stop and kill-mid-upload journeys *look*
like they need no server, because the server being absent is exactly what they are
about. **They still cannot run without one.** To have work waiting in the queue you must
first *have* work, and the work board is composed by the backend bootstrap — with no
server there is nothing on the board to record. The transition from online to offline
needs the server to have been there first.

So they are parked with the rest. This is the single biggest repeat-bug pattern in the
lane (offline-sync-queue 108 commits, proof/media 564) and it stays uncovered until
there is a backend it is safe to point at. Saying so is the point.

## The budget rule

Firebase Test Lab's free tier on `goatos-stg` is **10 virtual tests and 60
device-minutes a day**, for the whole project. Past that Google bills **$1 per
device-hour** for a virtual device and **$5 per device-hour** for a physical one.
The free tier grants **no physical device time at all**.

**Nothing in this lane may incur billable device time.** The rule is enforced in
code, in `freeTierVerdict()` in `run-android-journeys.mjs`, not in a comment:

- A matrix with more than 10 tests is **refused**, naming the number.
- A matrix whose worst case (tests × timeout) exceeds 60 device-minutes is
  **refused**.
- A run is refused if what is already spent today leaves no room — it is parked
  until tomorrow rather than squeezed in.
- `--physical` is **refused outright**.
- There is deliberately **no override flag**. Deciding to spend money is Ravi's
  call, not this runner's and not an agent's.

The guard **never trims** an oversized matrix to make it fit. A run that quietly
dropped half its journeys would report a green covering less than it claims.

Today's spend is kept in `.codex-goatos-render/android-journeys/free-tier-spend.json`,
which resets on a new day and, if it cannot be read, resets to zero for today rather
than being read as "plenty left".

## Running it

The layer is **default OFF** — `GOATOS_DASHBOARD_ANDROID_JOURNEYS`. It is nightly
or on demand, never on every run, because the daily allowance belongs to the whole
project.

```bash
# plan and check the budget, submit nothing
node tools/dashboard-automation/run-android-journeys.mjs --dry-run

# one journey
node tools/dashboard-automation/run-android-journeys.mjs --only login-and-session --dry-run

# a real run (see "The live run is parked" below for why this is not yet possible here)
node tools/dashboard-automation/run-android-journeys.mjs \
  --app-apk  apps/goatos-android/app/build/outputs/apk/dev/debug/app-dev-debug.apk \
  --test-apk apps/goatos-android/journeys/build/outputs/apk/dev/debug/journeys-dev-debug.apk \
  --out .codex-goatos-render/android-journeys/receipt.json

# inside the whole automation run
GOATOS_DASHBOARD_ANDROID_JOURNEYS=1 node tools/dashboard-automation/run.mjs --mode post-main-certification
```

Self-tests and tests:

```bash
node tools/dashboard-automation/run-android-journeys.mjs --self-test
node --test tools/dashboard-automation/run-android-journeys.test.mjs
node tools/dashboard-automation/notify-slack.mjs --self-test
node tools/dashboard-automation/check-static-inventory.mjs --self-test
```

## How the tests drive the phone

`apps/goatos-android/journeys` is a `com.android.test` module shaped like
`:benchmark`, with one deliberate difference: it builds against the **debug** build
type, not `benchmark`. The app's own debug affordances live in `app/src/debug/` and
merge into `debug` only; `benchmark` is `initWith(release)` and cannot see them.
That is exactly why `:benchmark` has to tap the bottom-bar labels and needs a live
backend bootstrap — those labels are composed by the server and translated
server-side, so they are not stable literals.

This suite instead uses what the app already owns:

| Lever | How | Why it is honest |
|---|---|---|
| Jump to a screen | the app's own debug navigation broadcast | goes through the real hosting and permission gates, and reports whether it actually navigated — a route the role may not see cannot be mistaken for a pass |
| Take the network away | airplane mode over the instrumentation shell | the network really is gone; the app has no force-offline flag to fake it with |
| Kill the app mid-flight | force-stop over the instrumentation shell | the same thing Android does under memory pressure |
| Scan a tag | the app's own debug RFID injection | drives the real reader port with real key events |

There is deliberately **no back door into the app's own state**. A journey that can
only be proven by reading the app's internals is not a journey a farm manager can
read.

Two tests in the suite claim **no catalogue check at all** and are documented as
such: one proves the sign-in screen is readable, the other proves the offline lever
actually reaches the app. They exist so the levers the sync journeys depend on are
known to work. A test counts as coverage only when the catalogue names it.

## What a finding looks like in Slack

All of lane 5's rendering lives in
`tools/dashboard-automation/lib/finding-kinds/android-journeys.mjs`. `notify-slack.mjs`
gains exactly **one import and one registry entry** — nothing else in that file
changes, and a test proves a lane-1-only receipt renders **byte-identically** with
lane 5 registered.

A finding says what a person holding the phone sees, names the screen, and carries
that screen's Test Lab screenshot:

> 📱 **2 things a person on the phone would hit**
>
> **On the phone**
> • **Proof upload / sync status** — A photo that was half-uploaded when the app closed is lost, so the job goes in with no proof attached.
> • **Auth / shell** — People are signed out at random, or stay signed in after their access was removed.
>
> _Checked on Firebase Test Lab virtual MediumPhone.arm / Android 33. Screenshot of each one below._

**No class names, test ids, selectors, stack traces or check codes.** Every string
that leaves that module goes through `plainEnglish()` first, and anything that trips
the leak detector is **replaced rather than printed** — Slack showing nothing useful
is recoverable; Slack showing a stack trace to a farm manager is not. All 47
`humanFailure` sentences are held to both that detector and the
`failureSentenceFindings` guard the rest of the automation already enforces, so this
lane is held to one standard rather than its own.

Evidence is pulled out of Test Lab into a GCS bucket, so a screenshot path can be a
`gs://` string rather than a file. `renderReplies` drops any path that is not a file
on local disk: handing Slack a bucket URL makes `readFileSync` throw inside
`postSlack`'s catch, where `path.basename` throws again, unhandled, *after* the
message has already posted.

## The physical-device set

These five are run **by hand on a real farm phone before a release**. A Firebase
Test Lab virtual device would report them green while proving nothing, and this lane
will never claim them from a virtual run. They carry `testClass: null` in the
catalogue so no code path can execute them, and `check-static-inventory.mjs` fails
the build if that ever changes.

**A photo or video can be captured and attached to the job it belongs to** (`proof-capture`, Proof capture)

> A stockman photographs or films the work he has just done, and that proof lands on the right job so the verifier can see it.

*Why a virtual device would be a false green:* Drives the real camera and the video encoder. A virtual device's fake camera produces a synthetic frame, so a green run would not prove a real capture works on a farm phone.

*What the farm sees if it breaks:* Photos and videos taken on the phone do not attach to the job, so the work cannot be checked.

---

**Feed transport capture records the load with its video** (`feed-transport-capture`, Feed transport capture)

> The video that proves a feed load actually moved is recorded on the phone and attached to the transport job.

*Why a virtual device would be a false green:* Records video through the device camera; a Test Lab virtual device has no real camera or encoder, so a pass there would prove nothing.

*What the farm sees if it breaks:* The video that proves feed was moved cannot be recorded on the phone.

---

**Scanning an ear tag brings up the right animal at the scale** (`weighing-scan-identifies-the-animal`, Weighing execution scan / Scan)

> A tag is scanned at the scale and the animal that comes up is the animal the tag is on, with its pen and its last weight.

*Why a virtual device would be a false green:* Reads a physical RFID ear tag through the handheld reader; a virtual device has no RFID hardware and cannot exercise the reader at all.

*What the farm sees if it breaks:* Scanning a tag at the scale brings up the wrong animal, so the weight is recorded against the wrong goat.

---

**Scanning a person's card clocks the right person in** (`roster-scan`, Roster scan)

> A card scanned at the gate clocks in the person whose card it is.

*Why a virtual device would be a false green:* Reads a physical card through the device's RFID/NFC hardware, which a Test Lab virtual device does not have.

*What the farm sees if it breaks:* Scanning a card at the gate clocks in the wrong person.

---

**Ear tag readings are picked up from the gateway** (`herd-signal-tags`, Herd signals)

> A tag on an animal is heard by the phone and shows up with a recent reading and a sensible battery level.

*Why a virtual device would be a false green:* Needs a real Bluetooth radio to hear a real tag; a Test Lab virtual device has no BLE stack and cannot see a physical beacon.

*What the farm sees if it breaks:* The live animal tracker shows nothing, so the tags on the animals are not being read.
## Where the APK is built, and where it is not

Firebase Test Lab supplies the **devices**. It does not build the app: it takes two
APKs — the app and the test — built on some other machine and uploaded. So "which
machine builds" and "can the run happen" are separate questions, and only the first
one has a constraint.

**The OCI box cannot build an Android APK.** The roadmap records it as "can still
build the APK if a JDK + SDK are installed". That is not right, and the reason is not
disk. `goatos-oci` is **aarch64**, and Google ships the Android build tools for
**x86-64 Linux only** — the `aapt2` in both the SDK's `build-tools/36.0.0` and
Gradle's own transform cache is an `ELF 64-bit ... x86-64` binary. A JDK 21 and the
full SDK were installed there and a build run: it reached `:app:assembleDevDebug` and
failed with `AAPT2 ... Daemon startup failed` on every resource-compiling module,
after 7m20s. There is no official aarch64 `aapt2` to point
`android.aapt2FromMavenOverride` at. The box was left as it was found.

**This laptop builds it.** The SDK here ships a universal `aapt2` with an arm64
slice, and `tools/ci/run-local-ci.sh` already builds this app with
`JAVA_HOME=/opt/homebrew/opt/openjdk@21`. That is the builder these journeys use:

```bash
cd apps/goatos-android
JAVA_HOME=/opt/homebrew/opt/openjdk@21 \
ANDROID_HOME="$HOME/Library/Android/sdk" \
  ./gradlew :app:assembleDevDebug :journeys:assembleDevDebug
```

**Cloud Build also builds signed Android APKs** for Firebase App Distribution, so it
is the other candidate builder if the test APK ever needs to be produced off a
laptop. Nothing in this lane triggers it.

## Everything parked, with its reason

Nothing below is reported as covered. 37 of them are parked **by decision**, not by
obstacle: they need a backend, and no backend may be used.

**`offline-queue-survives-force-stop`** — The test is written and the levers it uses are proven to work by the cold-boot set. What is missing is somewhere safe to write. No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`sync-retries-and-recovers`** — The app has no hook to make the server answer with an error, so there is no honest way to drive this from a virtual device. Either the app grows a debug fault-injection affordance, or this stays a manual check. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`conflict-does-not-lose-work`** — Needs the same job edited on the web while the phone is offline. That is a two-actor journey; lane 4 owns the write side and this should be built jointly with it rather than faked on the phone alone. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`sync-status-screen`** — The test is written and the levers it uses are proven to work by the cold-boot set. What is missing is somewhere safe to write. No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`upload-killed-mid-flight-resumes`** — The test is written and the levers it uses are proven to work by the cold-boot set. What is missing is somewhere safe to write. No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`low-storage-is-handled`** — Filling a Test Lab device's storage to provoke the real out-of-space path risks wedging the device mid-run and burning the day's quota. Needs a debug affordance that reports storage as full. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`proof-media-playback`** — Playback can be driven on a virtual device, but the only proof to play is one a real camera recorded, so it is built alongside the physical proof-capture set. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`work-board-and-submit`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`shifting-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`weighing-capture-screen`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`verify-queue`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`work-instructions`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`pen-reconciliation`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`pc-care-task`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`shell-navigation`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`app-update-gate`** — The app skips its own out-of-date-build gate on debug builds (the gate is set to fail open there), and the suite has to run a debug build to reach the app's debug navigation receiver. So the gate cannot be driven on the build this lane can run. Driving it needs either a release-flavour build with a seeded session, or a debug affordance the app already has for forcing the gate but which only applies at launch.

**`push-notification-opens-the-right-job`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`animal-purchase-load`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`feed-distribution-complete`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`vendors-and-feed-purchases`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`leadership-tasks-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`feed-direction-worklist`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`feed-packing-complete`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`sales-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`health-and-toxin-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`approvals-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`add-a-birth-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`add-a-death-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`clock-and-leave`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`weighing-task-detail-and-sheds`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`verify-detail-and-verdict`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`capture-access-gate`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks.

**`calendar-on-the-phone`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`weighing-fasting-and-operators`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`pen-routine-and-pen-visit`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`vaccination-submit`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`feed-wastage`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`weighing-plan-wizard`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`market-survey`** — Catalogued from the commit ledger, not yet automated. The sync architecture and the upload-failure set are built first, as the roadmap asks. It also needs a backend: No Test Lab-reachable non-production backend exists. The dev flavour points at http://localhost:8080/, which a device in Google's cloud cannot reach, and the stg and prod flavours point at the deployed stg API and at production, which carries real farm data - these journeys WRITE, so they may never run against either. Fixed by exposing the OCI writable clone's API to Test Lab, or by shipping a stub server the dev build can be pointed at with -PgoatosDevApiBaseUrl. Until then this is parked, not covered. PARKED BY DECISION, not by obstacle. Ravi's instruction: no write-path journey runs against any real backend - not the stg API, not production, not the OCI clone, not a local backend. It is catalogued here as data, with its story and its assertion, and it is not executed. Parked is the honest outcome and it is preferred to anything that goes near real farm data.

**`language-switch`** — Checking a translation needs the device locale set to Hindi, Kannada or Telugu and the translated strings asserted. That is one Test Lab test per language out of the ten a day, and a set of expected strings per language that does not exist yet. Asserting the ENGLISH strings are present, which is what the cold-boot suite does, is not this check and is not counted as it.

## The 47 journeys, in build order

Ordered by the team's own repeat bugs, worst first. Proof/media and the offline sync
queue are the two biggest in this lane, so the sync architecture and the upload-failure
set lead and everything else follows by how much work has gone into it. The full story,
flow and assertion for each is in `tools/dashboard-automation/android-journeys.json`.

| # | Journey | Screen | Repeat bugs behind it | Commits | Where it can run | Status |
|---:|---|---|---|---:|---|---|
| 1 | `offline-queue-survives-force-stop` | Sync / any capture screen | offline-sync-queue, idempotency-outbox-replay | 100 | parked | parked |
| 2 | `sync-retries-and-recovers` | Sync status | proof-media-missing, idempotency-outbox-replay | 41 | parked | parked |
| 3 | `conflict-does-not-lose-work` | Sync status / any task detail | stuck-or-rolled-forward-work, proof-media-missing | 19 | parked | parked |
| 4 | `sync-status-screen` | Sync status | verification-gate | 1 | parked | parked |
| 5 | `upload-killed-mid-flight-resumes` | Proof upload / sync status | proof-media-missing, verification-gate | 15 | parked | parked |
| 6 | `low-storage-is-handled` | Proof capture | proof-media-missing, partition-pen-label | 10 | parked | parked |
| 7 | `proof-media-playback` | Proof media preview | proof-media-missing, verification-gate | 56 | parked | parked |
| 8 | `proof-capture` | Proof capture | proof-media-missing, verification-gate | 125 | farm phone | physical |
| 9 | `feed-transport-capture` | Feed transport capture | — | 0 | farm phone | physical |
| 10 | `work-board-and-submit` | Work board / Submit | partition-pen-label, double-count-duplicate | 134 | parked | parked |
| 11 | `shifting-on-the-phone` | Shifting / Shifting execute | proof-media-missing, published-version-not-locked | 100 | parked | parked |
| 12 | `weighing-capture-screen` | Weighing capture | proof-media-missing, notification-not-delivered | 73 | parked | parked |
| 13 | `verify-queue` | Verify queue | verification-gate, double-count-duplicate | 42 | parked | parked |
| 14 | `work-instructions` | Work instructions | proof-media-missing, verification-gate | 37 | parked | parked |
| 15 | `pen-reconciliation` | Pen reconciliation | totals-do-not-reconcile, proof-media-missing | 36 | parked | parked |
| 16 | `login-and-session` | Auth / shell | proof-media-missing, verification-gate | 34 | Test Lab | **runs** |
| 17 | `weighing-scan-identifies-the-animal` | Weighing execution scan / Scan | proof-media-missing, verification-gate | 33 | farm phone | physical |
| 18 | `pc-care-task` | PC care task / plan / monitor | proof-media-missing, null-empty-leak | 31 | parked | parked |
| 19 | `shell-navigation` | GoatOS shell | proof-media-missing, verification-gate | 28 | parked | parked |
| 20 | `app-update-gate` | Boot / update gate | proof-media-missing | 17 | Test Lab | parked |
| 21 | `push-notification-opens-the-right-job` | Push / notifications | notification-not-delivered, verification-gate | 15 | parked | parked |
| 22 | `animal-purchase-load` | Animal purchase load detail | proof-media-missing, published-version-not-locked | 13 | parked | parked |
| 23 | `feed-distribution-complete` | Feed distribution complete | status-machine-drift, proof-media-missing | 12 | parked | parked |
| 24 | `vendors-and-feed-purchases` | Vendor create / Feed purchase create and detail | verification-gate, published-version-not-locked | 12 | parked | parked |
| 25 | `leadership-tasks-on-the-phone` | Leadership task list / detail | status-machine-drift | 11 | parked | parked |
| 26 | `feed-direction-worklist` | Feed direction | status-machine-drift, published-version-not-locked | 7 | parked | parked |
| 27 | `feed-packing-complete` | Feed packing complete | proof-media-missing, totals-do-not-reconcile | 7 | parked | parked |
| 28 | `sales-on-the-phone` | Sales lead board / Sale create | verification-gate | 7 | parked | parked |
| 29 | `health-and-toxin-on-the-phone` | Health / Toxin | proof-media-missing | 6 | parked | parked |
| 30 | `approvals-on-the-phone` | Approvals / Approval | proof-media-missing, double-count-duplicate | 5 | parked | parked |
| 31 | `add-a-birth-on-the-phone` | Add birth | published-version-not-locked, proof-media-missing | 4 | parked | parked |
| 32 | `add-a-death-on-the-phone` | Add death | partition-pen-label | 3 | parked | parked |
| 33 | `clock-and-leave` | Clock / Leave | — | 3 | parked | parked |
| 34 | `crash-free-on-the-covered-screens` | (every screen in this lane) | — | 3 | Test Lab | **runs** |
| 35 | `weighing-task-detail-and-sheds` | Sheds / Weighing task detail | partition-pen-label | 2 | parked | parked |
| 36 | `verify-detail-and-verdict` | Verify detail | verification-gate, proof-media-missing | 2 | parked | parked |
| 37 | `capture-access-gate` | Capture access gate | permission-or-access-drift, stuck-or-rolled-forward-work | 2 | Test Lab | parked |
| 38 | `calendar-on-the-phone` | Calendar | — | 2 | parked | parked |
| 39 | `weighing-fasting-and-operators` | Weighing fasting detail / Weighing operators | proof-media-missing | 1 | parked | parked |
| 40 | `pen-routine-and-pen-visit` | Pen routine detail / pen visits | — | 1 | parked | parked |
| 41 | `vaccination-submit` | Vaccination submit | — | 1 | parked | parked |
| 42 | `feed-wastage` | Feed wastage complete | — | 0 | parked | parked |
| 43 | `weighing-plan-wizard` | Weighing plan wizard | — | 0 | parked | parked |
| 44 | `roster-scan` | Roster scan | — | 0 | farm phone | physical |
| 45 | `market-survey` | Market survey / city entry | — | 0 | parked | parked |
| 46 | `language-switch` | Shell / any screen | — | 0 | Test Lab | parked |
| 47 | `herd-signal-tags` | Herd signals | — | 0 | farm phone | physical |

## A footnote on the OCI box

If the OCI box ever *has* to be the builder, running the x86-64 Android build tools
there under `qemu-user-static` would in principle work. It is **untried**: the package
is not in Oracle Linux 9's base repositories, so it needs EPEL plus a `binfmt_misc`
registration — a system-level change to a shared box — and a full Android build under
instruction emulation on four cores is slow. Written down as an option, not a plan.
It is not needed while the laptop and Cloud Build can both produce the APK.
