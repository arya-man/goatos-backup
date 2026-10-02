import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

// pr294 L-C12: /verify?status=all (the leadership link) rendered no selected tab: the queue offers
// no All chip, so nothing matched value "all".
test("a ?status=all view gets its own selected All tab", () => {
  assert.match(page, /status === "all" && !allStatusOption\s*\?\s*\[\{ value: "all", label: copy\(pageContract, "tab\.all_statuses"\)/);
  assert.match(service, /"tab\.all_statuses":\s+"All"/);
});

// pr294 L-C12: the queue ran 1,230-1,255px in the 1,060px card at 1440, cutting Reason.
test("queue dates stack over their time and cells take 12px gutters from md", () => {
  assert.match(page, /cell\(stackedDateTime\(item\.captured_at\)\)/);
  assert.match(page, /item\.verified_at \? stackedDateTime\(item\.verified_at\)/);
  assert.match(page, /"& \.MuiTableCell-root": \{ px: \{ md: 1\.5 \} \}/);
  assert.match(page, /columnKeys\[index\] === "review_took" \? \{ whiteSpace: "normal" \}/);
});
