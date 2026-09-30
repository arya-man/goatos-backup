/**
 * A server-action form's hidden value (idempotency key, return_to, row ids).
 *
 * The one place a feature's `<form>` gets a native hidden input: it has no UI, so there is no MUI
 * part for it, and keeping it here lets feature files stay free of native controls (guard
 * `legacy-free-zone`). Works from server and client components alike.
 */
export function HiddenField({ name, value }: { name: string; value: string | number | readonly string[] | undefined }) {
  return <input type="hidden" name={name} value={value ?? ""} />;
}
