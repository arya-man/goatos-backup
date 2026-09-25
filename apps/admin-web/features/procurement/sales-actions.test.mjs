import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./sales-actions.ts", import.meta.url), "utf8");
const updatePaymentBlock = source.slice(
  source.indexOf("export async function updateSalesDealPaymentAction"),
  source.indexOf("export async function deleteSalesDealPaymentAction"),
);
const deletePaymentBlock = source.slice(
  source.indexOf("export async function deleteSalesDealPaymentAction"),
  source.indexOf("/** Sets a deal's lifecycle status"),
);

test("payment edit and delete actions take the form's key, never a content hash", () => {
  // A content-derived key treats a later edit back to an earlier value as an
  // idempotent replay, so the backend skips the mutation and leaves the newer
  // value in place. The key is the FORM's (payment-idempotency.ts): one per
  // rendered form, rotated once an edit lands, so each edit is a new intent
  // while a double click of the same form stays one.
  assert.match(updatePaymentBlock, /updateSalesDealPayment\([\s\S]*paymentIdempotencyKey\(formData\),[\s\S]*\);/);
  assert.match(deletePaymentBlock, /deleteSalesDealPayment\([\s\S]*paymentIdempotencyKey\(formData\)\);/);
  assert.doesNotMatch(source, /stableSalesActionKey/);
  assert.doesNotMatch(source, /createHash/);
});
