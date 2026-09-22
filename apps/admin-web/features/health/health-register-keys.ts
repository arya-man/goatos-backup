/**
 * Mints an idempotency key for ONE human intent.
 *
 * Called from an event handler, never during render. A key generated while rendering changes on
 * every re-render, so a retry after a network failure would carry a different key and the write
 * would land twice -- the exact opposite of what the key is for. React's purity rule and this
 * write-path rule happen to be the same rule here.
 */
export function mintKey(prefix: string): string {
  const random =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2);
  return `${prefix}-${random}`;
}
