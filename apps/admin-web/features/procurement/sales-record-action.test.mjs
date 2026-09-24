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
      randomUUID: () => "test-key",
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
