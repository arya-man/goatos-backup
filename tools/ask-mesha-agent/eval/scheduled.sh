#!/usr/bin/env bash
# eval/scheduled.sh: timer entry point for the Ask Mesha accuracy regression (eval/run.mjs).
# Cost: ~$0.15 per question, charged to the agent's $100 MONTHLY cap (bench asks count like any ask).
#   weekly full run (42 q) ~= $6.3/run ~= $25-27/month. Daily runs are OFF by default.
#
# Env (put secrets in $ASK_MESHA_EVAL_ENV, default $ASK_MESHA_STATE_DIR/.eval.env, chmod 600):
#   ASK_MESHA_STATE_DIR        agent state dir (.pgenv, evals/, events) - default ~/.ask-mesha-agent
#   ASK_MESHA_BENCH_TOKEN      bench bearer of the running agent (local only; never set on Cloud Run)
#   ASK_MESHA_URL              agent base, default http://127.0.0.1:8787
#   ASK_MESHA_EVAL_SUBSET      "" = all (default), N = first N, or tag/id list e.g. "core" or "trap,adg-by-park"
#   ASK_MESHA_EVAL_BUDGET_USD  stop asking once spend passes this (default 7)
#   ASK_MESHA_EVAL_CONCURRENCY default 2
#   ASK_MESHA_EVAL_BEARER / ASK_MESHA_EVAL_TENANT  optional UI (admin-web backend) leg; ID tokens expire in 1h,
#                              so a timer run normally skips it (noted in the report).
# Exit code = run.mjs exit code (1 = a golden failed). Report: $ASK_MESHA_STATE_DIR/evals/<ts>.json
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE="${ASK_MESHA_STATE_DIR:-$HOME/.ask-mesha-agent}"
ENV_FILE="${ASK_MESHA_EVAL_ENV:-$STATE/.eval.env}"
# shellcheck disable=SC1090
[ -f "$ENV_FILE" ] && { set -a; . "$ENV_FILE"; set +a; }
export ASK_MESHA_STATE_DIR="$STATE"
args=(--budget-usd "${ASK_MESHA_EVAL_BUDGET_USD:-7}" --concurrency "${ASK_MESHA_EVAL_CONCURRENCY:-2}")
[ -n "${ASK_MESHA_EVAL_SUBSET:-}" ] && args+=(--subset "$ASK_MESHA_EVAL_SUBSET")
exec node "$SCRIPT_DIR/run.mjs" "${args[@]}" "$@"
