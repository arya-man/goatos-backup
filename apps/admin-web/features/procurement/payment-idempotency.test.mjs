import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";
import ts from "typescript";

// Defect 2026-09-25: a double click on "Record payment" recorded the money twice, because the
// Server Action minted a fresh idempotency key on EVERY call. These tests execute the REAL actions
// with only their network/framework boundaries replaced, and a randomUUID that -- like the real
// one -- returns a new value per call. Two submits of the same rendered form must reach the
// backend with ONE key: the key the form was rendered with.

const actionsSource = readFileSync(new URL("./sales-actions.ts", import.meta.url), "utf8");
const keySource = readFileSync(new URL("./payment-idempotency.ts", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./sales-record-drawer.tsx", import.meta.url), "utf8");

function keyModule() {
  const context = vm.createContext({});
  vm.runInContext(ts.transpile(keySource.replace(/^export /gm, "")), context);
  return context;
}

function loadAction(name) {
  const start = actionsSource.indexOf(`export async function ${name}`);
  assert.notEqual(start, -1, `${name} not found`);
  const end = actionsSource.indexOf("\nexport ", start + 1);
  const helperStart = actionsSource.indexOf("function paymentRefusal");
  const helper = helperStart === -1 ? "" : actionsSource.slice(helperStart, actionsSource.indexOf("\n}\n", helperStart) + 3);
  const js = ts.transpile(helper + actionsSource.slice(start, end === -1 ? undefined : end).replace("export ", ""));
  const keys = [];
  const record = (key) => {
    keys.push(key);
    return { ok: true, data: {} };
  };
  let minted = 0;
  const helpers = keyModule();
  const context = vm.createContext({
    recordSalesDealPayment: async (_deal, _body, key) => record(key),
    updateSalesDealPayment: async (_deal, _payment, _body, key) => record(key),
    deleteSalesDealPayment: async (_deal, _payment, key) => record(key),
    randomUUID: () => `per-call-${++minted}`,
    requiredString: (form, key) => {
      const value = form.get(key)?.toString() ?? "";
      if (!value) throw new Error(`${key} is required`);
      return value;
    },
    paymentIdempotencyKey: helpers.paymentIdempotencyKey,
    SALES_PATH: "/sales/config",
    revalidatePath: () => {},
    actionRedirect: () => {},
  });
  vm.runInContext(js, context);
  return { run: context[name], keys };
}

function paymentForm(key) {
  const form = new FormData();
  form.set("return_to", "/sales/config?deal_id=d1");
  form.set("deal_id", "d1");
  form.set("payment_id", "p1");
  form.set("received_on", "2026-09-20");
  form.set("amount_rupees", "5000");
  if (key) form.set("idempotency_key", key);
  return form;
}

for (const name of ["recordSalesDealPaymentAction", "updateSalesDealPaymentAction", "deleteSalesDealPaymentAction"]) {
  test(`${name}: two submits of the same form carry the form's one key`, async () => {
    const { run, keys } = loadAction(name);
    const form = paymentForm("form-key-1");
    // A double click: the same rendered form posted twice before it re-renders.
    await Promise.all([run(form), run(form)]);
    assert.deepEqual(keys, ["form-key-1", "form-key-1"]);
  });

  test(`${name}: a form without a key is refused rather than given one per call`, async () => {
    const { run, keys } = loadAction(name);
    await assert.rejects(run(paymentForm("")), /idempotency_key is required/);
    assert.deepEqual(keys, []);
  });
}

test("recordSalesDealPaymentAction returns the backend's field sentence in place", async () => {
  const start = actionsSource.indexOf("function paymentRefusal");
  assert.notEqual(start, -1);
  const js = ts.transpile(actionsSource.slice(start, actionsSource.indexOf("\n}\n", start) + 3));
  const context = vm.createContext({});
  vm.runInContext(js, context);
  assert.deepEqual(
    { ...context.paymentRefusal({ code: "sales_invalid_received_on", message: "Received on cannot be in the future." }) },
    { code: "sales_invalid_received_on", message: "Received on cannot be in the future." },
  );
  // Not a field refusal: the transport's text is not farm copy, so the form shows its own.
  assert.deepEqual(
    { ...context.paymentRefusal({ message: "Backend service returned 500." }) },
    { code: "", message: "" },
  );
});

test("the key holds while the form's outcome holds, and moves when it moves", () => {
  const { paymentKeyFor } = keyModule();
  let n = 0;
  const mint = () => `k${++n}`;
  const first = paymentKeyFor(null, "p1#0", mint);
  assert.equal(first.key, "k1");
  // Re-render with nothing settled: same key, however many clicks.
  assert.equal(paymentKeyFor(first, "p1#0", mint), first);
  // A receipt landed (the payment list changed) or an attempt settled: a new intent, a new key.
  assert.equal(paymentKeyFor(first, "p1,p2#0", mint).key, "k2");
  assert.equal(paymentKeyFor(first, "p1#1", mint).key, "k3");
});

test("every payment form is rendered with its key and cannot be submitted while in flight", () => {
  // Record, edit and remove each carry the hidden key field -- and so does the record-sale form.
  assert.equal((drawerSource.match(/name=\{PAYMENT_IDEMPOTENCY_FIELD\}/g) ?? []).length, 4);
  assert.match(drawerSource, /usePaymentFormAction\(recordSalesDealPaymentAction/);
  assert.match(drawerSource, /usePaymentFormAction\(\s*updateSalesDealPaymentAction/);
  assert.match(drawerSource, /usePaymentFormAction\(deleteSalesDealPaymentAction/);
  assert.match(drawerSource, /className="btn p" disabled=\{pending\}/);
  assert.equal((drawerSource.match(/\sdisabled=\{busy\}/g) ?? []).length, 2);
  // Submitted through onSubmit, never an action prop: React resets an action-prop form once the
  // action settles, which wiped a refused receipt's typed amount and note (seen on the live proof).
  assert.match(drawerSource, /<form onSubmit=\{onSubmit\} aria-busy=\{pending\}>/);
  assert.match(drawerSource, /<form id=\{editFormId\} onSubmit=\{edit\.onSubmit\} hidden>/);
  assert.match(drawerSource, /<form id=\{deleteFormId\} onSubmit=\{remove\.onSubmit\} hidden>/);
  // The key is never minted inside the Server Actions.
  const paymentActions = actionsSource.slice(actionsSource.indexOf("export async function recordSalesDealPaymentAction"));
  assert.doesNotMatch(paymentActions.slice(0, paymentActions.indexOf("/** Sets a deal's lifecycle status")), /randomUUID/);
});
