import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";
import ts from "typescript";

// Execute the real action with its network and framework boundaries replaced.
for (const code of ["feed_stock_confirmation_required", "validation_error"]) {
  test(`${code} returns in place without redirecting or revalidating`, async () => {
    const source = readFileSync(new URL("./sales-actions.ts", import.meta.url), "utf8");
    const start = source.indexOf("export async function recordSaleAction");
    const end = source.indexOf("\n/**", start);
    const js = ts.transpile(source.slice(start, end).replace("export ", ""));
    let writes = 0;
    const context = vm.createContext({
      createSalesDeal: async () => { writes++; return { ok: false, error: { code, message: "Check this feed" } }; },
      readSaleForm: (form) => Object.fromEntries(form),
      paymentIdempotencyKey: () => "test-key",
      revalidatePath: () => { throw new Error("unexpected revalidation"); },
      actionRedirect: () => { throw new Error("unexpected redirect"); },
      actionRedirectWithDetail: () => { throw new Error("refusal must preserve the form"); },
    });
    vm.runInContext(js, context);
    const draft = new FormData();
    draft.set("line_quantity_0", "2500");
    const result = await context.recordSaleAction(draft);
    assert.equal(result.code, code);
    assert.equal(result.message, "Check this feed");
    assert.equal(writes, 1);
  });
}

// Defect 2026-09-25 (the record-SALE twin of the receipt double-submit): the key was minted per
// call inside the action, and the drawer's pending check read a render-time value two clicks in
// one frame both saw as false -- so a double click could record the sale twice.
test("two submits of the same record-sale form carry the form's one key", async () => {
  const source = readFileSync(new URL("./sales-actions.ts", import.meta.url), "utf8");
  const keySource = readFileSync(new URL("./payment-idempotency.ts", import.meta.url), "utf8");
  const start = source.indexOf("export async function recordSaleAction");
  const end = source.indexOf("\n/**", start);
  const keys = [];
  let minted = 0;
  const helpers = vm.createContext({});
  vm.runInContext(ts.transpile(keySource.replace(/^export /gm, "")), helpers);
  const context = vm.createContext({
    createSalesDeal: async (_body, key) => { keys.push(key); return { ok: true, data: {} }; },
    readSaleForm: (form) => Object.fromEntries(form),
    randomUUID: () => `per-call-${++minted}`,
    paymentIdempotencyKey: helpers.paymentIdempotencyKey,
    SALES_PATH: "/sales/config",
    revalidatePath: () => {},
    actionRedirect: () => {},
    actionRedirectWithDetail: () => {},
  });
  vm.runInContext(ts.transpile(source.slice(start, end).replace("export ", "")), context);
  const form = new FormData();
  form.set("idempotency_key", "sale-form-key");
  await Promise.all([context.recordSaleAction(form), context.recordSaleAction(form)]);
  assert.deepEqual(keys, ["sale-form-key", "sale-form-key"]);
});

test("the record-sale drawer guards a same-frame double click and clears a stale stock tick", () => {
  const drawer = readFileSync(new URL("./sales-record-drawer.tsx", import.meta.url), "utf8");
  // The guard is a ref read at the click, not the render's pending value.
  assert.match(drawer, /if \(recordPendingRef\.current\) return;\s*recordPendingRef\.current = true;/);
  assert.match(drawer, /name=\{PAYMENT_IDEMPOTENCY_FIELD\} value=\{saleKey\}/);
  // The short-stock tick is controlled and cleared whenever the lines or the farm change.
  assert.match(drawer, /checked=\{stockAck\}/);
  assert.match(drawer, /onChange=\{changeLines\}/);
  assert.match(drawer, /const changeLines = \(next: SaleLineDraft\[\]\) => \{\s*setLines\(next\);\s*setStockAck\(false\);/);
  // MUI redesign: the farm picker is the kit FormSelect, whose change callback is onValueChange.
  assert.match(drawer, /id="s-farm"[^>]*onValueChange=\{\(\) => setStockAck\(false\)\}/);
});

test("a refused record-sale brings its reason into view", () => {
  // Save is at the drawer's foot and the refusal at the form's head: the drawer must scroll to it,
  // or the desk presses Save and sees nothing change.
  const drawer = readFileSync(new URL("./sales-record-drawer.tsx", import.meta.url), "utf8");
  assert.match(drawer, /ref=\{recordAlertRef\} role="alert"/);
  assert.match(drawer, /if \(recordError\) recordAlertRef\.current\?\.scrollIntoView\(/);
});

test("the sale's lines table pans inside the drawer instead of painting past it", () => {
  const drawer = readFileSync(new URL("./sales-record-drawer.tsx", import.meta.url), "utf8");
  assert.match(drawer, /<div className="twrap"[^>]*>\s*<Table className="sales-lines-table"/);
});
