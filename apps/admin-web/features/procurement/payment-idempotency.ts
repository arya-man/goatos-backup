// The idempotency key of one receipt write (record / edit / remove a deal payment).
//
// THE KEY BELONGS TO THE FORM, NOT TO THE SUBMIT (defect 2026-09-25). It used to be minted inside
// the Server Action on every call, so a double click on "Record payment" posted twice with two
// different keys and the backend -- correctly -- recorded two receipts: the same money counted
// twice against the buyer. The backend already honours an exact replay (same key, same payload ->
// the original result, no second row); it just never saw one.
//
// So the drawer mints the key ONCE when the form is shown and sends it as a hidden field. Every
// submit of that same form carries the same key, however many times it is clicked. The key moves
// on only when the form's outcome moves on -- a receipt landed (the deal's payment list changed)
// or an attempt settled -- so a genuine second receipt, or a corrected retry, is a new intent with
// a new key. It is never derived from the CONTENT of the form: an edit back to an earlier value is
// a new edit, and a content hash would replay it as the old one.

/** The hidden form field that carries the key. */
export const PAYMENT_IDEMPOTENCY_FIELD = "idempotency_key";

/**
 * Reads the key a payment form was rendered with. A form without one is refused rather than given
 * a fresh key here: a key minted per call is exactly the defect this file exists to prevent.
 */
export function paymentIdempotencyKey(formData: FormData): string {
  const key = (formData.get(PAYMENT_IDEMPOTENCY_FIELD)?.toString() ?? "").trim();
  if (key === "") {
    throw new Error(`${PAYMENT_IDEMPOTENCY_FIELD} is required`);
  }
  return key;
}

/** The key a form currently holds, and the outcome it was minted for. */
export type MintedPaymentKey = { rotation: string; key: string };

/**
 * The key a form should hold for `rotation` (its current outcome): the SAME key while the outcome
 * has not moved, a newly minted one once it has. Pure, so the rule is testable without React.
 */
export function paymentKeyFor(
  current: MintedPaymentKey | null,
  rotation: string,
  mint: () => string,
): MintedPaymentKey {
  if (current !== null && current.rotation === rotation) return current;
  return { rotation, key: mint() };
}
