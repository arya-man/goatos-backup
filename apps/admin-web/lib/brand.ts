/** The product name every rendered string uses. Goat OS / VGoat are internal identifiers only. */
export const PRODUCT_NAME = "Mesha";

/** Per-viewer browser storage keys share one product prefix (`mesha.<area>.<key>`). */
export function storageKey(area: string, key: string): string {
  return `mesha.${area}.${key}`;
}
