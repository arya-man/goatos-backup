/**
 * What a Sales page's error alert SAYS (2026-09-25): never the raw `code` / `kind` a failed read
 * carries -- `sales_invalid_farm` or `backend_down` is not farm copy. A refusal the backend worded
 * itself (it carries a code, so its message is the envelope's farm sentence) is shown as written;
 * a transport failure ("Backend service returned 500.", no code) gets the page contract's own
 * error copy. The code stays in the logs and telemetry, never on screen.
 */
export function salesErrorText(error: { code?: string; message?: string }, fallback: string): string {
  return error.code && error.message ? error.message : fallback;
}
