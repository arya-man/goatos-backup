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


## 21 September 2026 full discussion consolidation

- Scope: add follow-up sections 13–19 covering multiple entities, rupee settlement, FarmChain public-code/TestNet evidence, Pera/Algorand, authorisation/validators, PostgreSQL versus blockchain, and vetted third-party organisations.
- Done: refreshed clean isolated checkout to origin/main `e3a8e04a3`; preserved original research and added dated corrections and source links. Historical pending entries above are snapshots; original documentation and naming follow-up previously landed through `e565e0d291b89ec4ae31d9003f028cca5ed9638b`.
- Current SHA: base above; final candidate and landed SHA recorded in the local git-directory companion after certification to avoid a self-referential commit hash.
- Proof: `git diff --check` and documentation scope/evidence self-tests required before commit; `make land-main` required on the final candidate/current main before push. Exact outcomes recorded in companion.
- Judge status: primary agent checked content against conversation and distinguishes observed test issuance, advertised features, inference and unverified claims; no external judge requested for this update.
- Before/after: original 12-section research expanded with later discussion and corrected deployment/control distinctions. Runtime performance metrics and browser/API/Android E2E not applicable to documentation-only edits.
- Known failures: none in this update. Pending: focused checks, commit, certified landing and remote SHA readback.
- Deployment state: none requested; none performed.
