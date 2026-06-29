#!/usr/bin/env bash
# Validate the ops-console mock BEFORE it is pushed.
# Hard gate — exits non-zero if the mock is broken, so push-mock.sh aborts.
#
# Tier 1 (always, no deps): node-syntax-check every inline <script>. A single
#   syntax error makes the browser drop the WHOLE script, killing every click
#   handler — this is the exact bug that shipped a dead nav. This tier blocks.
# Tier 2 (if playwright present): load the mock in headless chromium, fail on
#   any JS error and click every nav/leaf asserting the screen actually changes.
set -uo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
F="$REPO/mock/goatos-dashboard-mock.html"
cd "$REPO" || exit 1

# Tier 1 — inline-script syntax (catches "one bad script kills all clicks").
node -e '
const fs=require("fs");
const h=fs.readFileSync(process.argv[1],"utf8");
const re=/<script\b[^>]*>([\s\S]*?)<\/script>/gi;
let m,i=0,bad=0;
const lineOf=(idx)=>h.slice(0,idx).split("\n").length;
while(m=re.exec(h)){i++;try{new Function(m[1]);}catch(e){bad++;console.error("  inline script #"+i+" ~line "+lineOf(m.index)+": "+e.message);}}
if(bad){console.error("[mock-syntax] FAIL — "+bad+" inline script(s) wont parse; ALL clicks are dead.");process.exit(1);}
console.log("[mock-syntax] OK — "+i+" inline scripts parse clean.");
' "$F" || exit 1

# Tier 2 — real click smoke (skips itself cleanly if playwright is absent).
node "$REPO/tools/agent-hooks/check-mock-clicks.mjs" || exit 1

exit 0
