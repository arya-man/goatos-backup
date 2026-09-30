#!/usr/bin/env bash
# ai-doctor: portability gate for the committed AI tooling stack.
#
# Why this exists: a fresh clone of goatos must run the agent hooks, skills, and
# runbooks WITHOUT editing absolute paths. This doctor is the regression guard
# that keeps the Tier A files clean after the 2026-06-29 hardcoded-path cleanup.
#
# Three lint rules (token-scoped, see AGENTS.md portability note):
#   1. Executables/config: forbid the maintainer-local home path — no legit external path
#      belongs in a script or the hook config; everything resolves repo-relative.
#   2. Active docs/skills: forbid the maintainer-local repo-root token — but
#      ALLOW other external maintainer-local references (e.g. the maintainer-
#      local mesha_docs_graph / wiki, which live OUTSIDE this repo and cannot be
#      made repo-relative). Frozen execution handoffs and specific frontend
#      screenshot ledgers are intentionally excluded from this portability gate.
#   3. Generated graph artifacts must stay gitignored and untracked.
#
# Plus a resolve-smoke: prove the repo-relative resolution actually finds the
# repo root from an unrelated cwd (catches an abspath removal that resolves to
# the wrong dir — the lint alone cannot see that).
#
# Usage: tools/agent-hooks/ai-doctor.sh   # exit 0 = ready, non-zero = breaker
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO" || exit 1

fail=0
note() { printf '%s\n' "$*"; }
LOCAL_USER_TOKEN="/Users/"'ravi'
REPO_ROOT_TOKEN="${LOCAL_USER_TOKEN}/mesha/goatos"

# Rule 1 — executables/config must contain NO maintainer-local home path.
EXEC_FILES=(
    .codex/hooks.json
    .claude/settings.json
    Makefile
    tools/agent-hooks/pre-search-guard.sh
    tools/agent-hooks/ai-setup-guard.sh
    tools/agent-hooks/pre-rtk-git-diff.sh
    tools/agent-hooks/pre-rtk-noisy-commands.sh
    tools/agent-hooks/rebuild-docs-graph.sh
    tools/agent-hooks/update-docs-graph.sh
    tools/agent-hooks/goatos-docs-corpus.sh
    tools/agent-hooks/ai-doctor.sh
    tools/agent-hooks/repowise-setup.sh
    tools/agent-hooks/repowise-coverage.sh
    tools/agent-hooks/install-stg-push-guard.sh
    tools/agent-hooks/pre-push.shim
    tools/agent-hooks/pre-push.hook
    tools/agent-hooks/pre-push.bundle
    tools/agent-docs/bootstrap-context7-secret.sh
    tools/agent-docs/ensure-context7.sh
    tools/agent-docs/refresh-if-needed.sh
    tools/ci/check-stg-promotion.mjs
    tools/ci/check-guardrail-registration.mjs
    tools/ci/check-local-ci-evidence.mjs
    tools/ci/land-main.sh
    tools/ci/land-main.test.sh
    tools/ci/guardrail-manifest.json
    tools/agent-hooks/check-mock.sh
    tools/agent-hooks/check-mock-clicks.mjs
    tools/agent-hooks/push-mock.sh
    tools/ai/analyze-transcripts.py
)

note "ai-doctor: portability lint (repo: $REPO)"

for f in "${EXEC_FILES[@]}"; do
    [ -f "$f" ] || { note "  MISSING (exec): $f"; fail=1; continue; }
    if grep -n "$LOCAL_USER_TOKEN" "$f" >/dev/null 2>&1; then
        note "  BREAKER (exec, no absolute path allowed): $f"
        grep -n "$LOCAL_USER_TOKEN" "$f" | sed 's/^/      /'
        fail=1
    fi
done

while IFS= read -r f; do
    [ -n "$f" ] || continue
    if grep -n "$REPO_ROOT_TOKEN" "$f" >/dev/null 2>&1; then
        note "  BREAKER (doc, repo-root path hardcoded): $f"
        grep -n "$REPO_ROOT_TOKEN" "$f" | sed 's/^/      /'
        fail=1
    fi
done < <(git ls-files '*.md' '*.mdc' | grep -Ev '^(context/execution/|context/frontend/herd-register-ui-fidelity-ledger\.md$|context/frontend/supplier-warmup-vaccination-gaps\.md$)' || true)

note "ai-doctor: generated graph artifact gate"
# Invariant (not an enumerated list): nothing under graphify-out may be tracked
# except its .gitignore, and nothing under .code-review-graph may be tracked.
# This catches NEW artifact filenames too, which an enumerated allowlist misses.
tracked_docs="$(git ls-files graphify-out | grep -v '^graphify-out/\.gitignore$' || true)"
tracked_crg="$(git ls-files .code-review-graph || true)"
tracked_repowise="$(git ls-files .repowise .mcp.json .vscode || true)"
tracked="$(printf '%s\n%s\n%s\n' "$tracked_docs" "$tracked_crg" "$tracked_repowise" | grep . || true)"
if [ -n "$tracked" ]; then
    note "  BREAKER: generated graph artifacts are tracked:"
    printf '%s\n' "$tracked" | sed 's/^/      /'
    fail=1
fi

# Ignore-side invariant: a brand-NEW generated filename must be ignored BY
# DEFAULT (whitelist .gitignore), not only the known artifact names.
for f in graphify-out/__ai_doctor_probe__.json .code-review-graph/__ai_doctor_probe__.db \
         .repowise/__ai_doctor_probe__.db .mcp.json; do
    if ! git check-ignore -q "$f"; then
        note "  BREAKER: new generated graph path is not ignored by default: $f"
        fail=1
    fi
done

# Runtime readiness — the setup is only useful if the tools are installed and
# this checkout has its local indexes. Doctor must not pass a half-installed AI
# stack and make the app look ready when CRG/Graphify/repowise are absent.
note "ai-doctor: runtime readiness"
require_cmd() {
    name="$1"
    if command -v "$name" >/dev/null 2>&1; then
        version="$("$name" --version 2>/dev/null | head -1 || true)"
        if [ -n "$version" ]; then
            note "  OK: $name ($version)"
        else
            note "  OK: $name ($(command -v "$name"))"
        fi
    else
        note "  MISSING: $name (run: make ai-setup)"
        fail=1
    fi
}
require_cmd code-review-graph
require_cmd graphify
require_cmd rtk
if [ "${REPOWISE_SETUP:-1}" != "0" ]; then
    require_cmd repowise
fi

is_head_or_parent() {
    index_commit="$1"
    [ -n "$index_commit" ] || return 1
    head_commit="$(git rev-parse HEAD 2>/dev/null || true)"
    parent_commit="$(git rev-parse HEAD^ 2>/dev/null || true)"
    if [ "$index_commit" = "$head_commit" ]; then
        return 0
    fi
    [ -n "$parent_commit" ] && [ "$index_commit" = "$parent_commit" ]
}

crg_db="$REPO/.code-review-graph/graph.db"
if [ -s "$crg_db" ]; then
    note "  OK: .code-review-graph/graph.db present"
else
    note "  MISSING: .code-review-graph/graph.db (run: make ai-setup)"
    fail=1
fi
if [ "${REPOWISE_SETUP:-1}" != "0" ]; then
    repowise_state="$REPO/.repowise/state.json"
    repowise_db="$REPO/.repowise/wiki.db"
    repowise_graph="$REPO/.repowise/knowledge-graph.json"
    if [ -s "$repowise_state" ] && [ -s "$repowise_db" ] && [ -s "$repowise_graph" ]; then
        repowise_commit="$(node -e 'const fs=require("fs"); const p=process.argv[1]; try { const s=JSON.parse(fs.readFileSync(p,"utf8")); process.stdout.write(s.last_sync_commit || ""); } catch (_) { process.exit(1); }' "$repowise_state" 2>/dev/null || true)"
        repowise_mode="$(node -e 'const fs=require("fs"); const p=process.argv[1]; try { const s=JSON.parse(fs.readFileSync(p,"utf8")); process.stdout.write(s.run_mode || ""); } catch (_) { process.exit(1); }' "$repowise_state" 2>/dev/null || true)"
        if [ -z "$repowise_commit" ]; then
            note "  MISSING: .repowise/state.json last_sync_commit (run: make ai-setup)"
            fail=1
        elif is_head_or_parent "$repowise_commit"; then
            note "  OK: .repowise index current enough (${repowise_commit}${repowise_mode:+, mode=$repowise_mode})"
        else
            note "  STALE: .repowise index at $repowise_commit, expected HEAD or HEAD^ (run: make ai-rebuild-repowise)"
            fail=1
        fi
    else
        note "  MISSING: .repowise index files state.json/wiki.db/knowledge-graph.json (run: make ai-setup)"
        fail=1
    fi
fi

# Resolve-smoke — repo-relative resolution must find the repo from any cwd.
note "ai-doctor: resolve smoke"
if ( cd /tmp && bash "$REPO/tools/agent-hooks/goatos-docs-corpus.sh" --matches AGENTS.md ) >/dev/null 2>&1; then
    note "  OK: goatos-docs-corpus.sh resolves repo root from foreign cwd"
else
    note "  FAIL: goatos-docs-corpus.sh did not resolve repo root from /tmp"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    note "ai-doctor: PASS — AI tooling is installed, indexed, clone-portable, and graph artifacts are local-only"
else
    note "ai-doctor: FAIL — run make ai-setup or fix the breakers above before relying on token-saving tools"
fi
exit "$fail"
