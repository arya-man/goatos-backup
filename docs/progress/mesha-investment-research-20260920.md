# Mesha investment research documentation progress

Date: 20 September 2026. Scope: preserve the initial financial-infrastructure problem, fifteen questions, supplied evaluations/audio, clarified combined-asset ownership model, research, corrections, source bibliography and lay explanation of three tokenisation precedents. Documentation only; no operating software, contract, fundraising or deployment change.

## Done

- Created isolated worktree from origin/main `6b2f3b470`; preserved dirty primary checkout.
- Read original proposal, both evaluations (PDF text extracted), audio transcript and repository landing rules.
- Documented mixed asset exposure, NAV/accounting, liquidity cash flows, India structure constraints and optional controlled blockchain.
- Parent research independently rechecked BUIDL, BENJI and Demat 2.0 primary sources; incorporated distinctions and limits.

## Proof and pending

- Exact focused checks (all passed): `git diff --check`; `node tools/ci/ci-scope.mjs --self-test`; `node tools/ci/check-local-ci-evidence.mjs --self-test`.
- Required promotion gate: `make land-main` after final committed edit and current-main refresh/rebase; gate determines current documentation-only scope and writes exact-SHA receipt.
- Current document is the pre-promotion snapshot. Final receipt/SHA and remote-main readback are recorded in the local companion progress log and reported with delivery; its outcome is not assumed here.
- Browser/API/Android E2E and latency metrics: not applicable, no runtime changes. Before/after financial metrics: unavailable, no Mesha accounts audited; examples are expressly illustrative.
- Judge status: parent researcher completed substantive QA and approved documentation for landing after verified redemption caveats, now incorporated; documentation agent cross-checked supplied sources. No software behaviour certification implied.
- Known failures: initial PDF text utility was unavailable; recovered using installed Python PDF extraction. No unresolved content-extraction blocker.
- Deployment state: no staging or production deploy requested or performed.

## 21 September 2026 naming follow-up

- Scope: user requested the exchange term; research title and opening now explicitly name **Mesha Goat Exchange**, preserving mixed-asset scope and staged trading model.
- Done: wording updated in the isolated checkout after refreshing origin/main.
- Current base SHA: `b00336e340fa293bb5bddc1ec0fe6b36b1f472c7`.
- Proof: `git diff --check` passes; exact final-SHA `make land-main` receipt is required before promotion and recorded in the local companion log.
- Pending: commit, required docs-only CI/landing gate, and remote-main readback.
- Known failures: none. Judge: wording reviewed directly against the user's naming request.
- Before/after: neutral investment-platform title changed to Mesha Goat Exchange; no runtime or financial-model changes. E2E/latency tests are not applicable. Deployment: none.
