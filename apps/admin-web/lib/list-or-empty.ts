/**
 * Go backends serialise an empty slice as `null`. Treat a missing/null list field as `[]` so pages
 * render their normal empty state instead of crashing on `.map`/`for…of`.
 */
export function listOrEmpty<T>(value: readonly T[] | null | undefined): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}
