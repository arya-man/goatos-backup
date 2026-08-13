/**
 * URL param names for the Actions board's capture-date filter.
 *
 * They live in their OWN module, with no "use client" directive, because both sides need them: the
 * server page reads them out of searchParams, and the client picker writes them. Exporting them
 * from the picker itself compiled and silently broke the page — Next hands a server component a
 * CLIENT REFERENCE PROXY for anything imported across the "use client" boundary, so
 * `one(sp, DATE_FROM_PARAM)` looked up a proxy object instead of the string "vd_from", found
 * nothing, and every selection fell back to today. Keep them here.
 *
 * A single day is the range [d, d]; a span is [from, to]. One encoding, so the page carries one
 * date concept and only decides at the query boundary whether it collapses to the backend's
 * `business_date` or opens into its range pair.
 */
export const DATE_FROM_PARAM = "vd_from";
export const DATE_TO_PARAM = "vd_to";
