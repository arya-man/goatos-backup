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

test("payment edit and delete actions mint a fresh key for each submit", () => {
  // A content-derived key treats a later edit back to an earlier value as an
  // idempotent replay, so the backend skips the mutation and leaves the newer
  // value in place. These row actions are standalone submits, so each submit is
  // a new editing intent and must carry a fresh key.
  assert.match(updatePaymentBlock, /updateSalesDealPayment\([\s\S]*randomUUID\(\),[\s\S]*\);/);
  assert.match(deletePaymentBlock, /deleteSalesDealPayment\([\s\S]*randomUUID\(\),[\s\S]*\);/);
  assert.doesNotMatch(source, /stableSalesActionKey/);
  assert.doesNotMatch(source, /createHash/);
});
