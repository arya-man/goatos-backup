// The one rule deciding whether a STAGED filter bar has anything left to apply. Kept in its own
// module, free of React and of "use client", so it can be unit-tested as the pure decision it is —
// the same reasoning as worklist-filter-value.ts beside it.
//
// WHY A BAR CAN BE STAGED AT ALL (Feed Config, maintainer report 2026-08-11): every control on the
// bar wrote the URL the moment it changed, so narrowing by four things ran four full server renders
// and showed three intermediate result sets nobody asked for — and on a page this heavy each one is
// a visible wait. A staged bar collects the edits locally and commits them once.
//
// The comparison MUST ignore the page parameter. Applying a filter resets paging, so a draft always
// drops the offset; comparing raw query strings would then report "something to apply" on a bar
// whose filters are untouched and whose reader is simply on page 3.

/**
 * A comparable key for a filter bar's query string: the page parameter removed, values sorted, so
 * two searches that ASK THE SAME QUESTION compare equal however they were assembled.
 *
 * Order-insensitive because the draft is rebuilt by mutation (`delete` then `append` moves a
 * repeated parameter to the end), and a re-ordered but identical set is not a change the operator
 * made. Repeated values are sorted as whole `key=value` pairs, so picking two feed items in the
 * other order is likewise not a change.
 */
export function worklistFilterSearchKey(search: string, pageParam: string): string {
  const params = new URLSearchParams(search);
  params.delete(pageParam);
  return [...params.entries()]
    .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
    .sort()
    .join("&");
}

/**
 * Whether the bar holds edits the operator has not applied yet — i.e. whether Apply is enabled.
 *
 * @param draftSearch  the staged query string, or null when nothing has been touched since the last
 *                     applied navigation.
 * @param appliedSearch the query string currently in force.
 * @param pageParam     the offset/page parameter this bar resets on apply, excluded from both sides.
 */
export function worklistFilterIsStaged(
  draftSearch: string | null,
  appliedSearch: string,
  pageParam: string,
): boolean {
  if (draftSearch === null) return false;
  return worklistFilterSearchKey(draftSearch, pageParam) !== worklistFilterSearchKey(appliedSearch, pageParam);
}
