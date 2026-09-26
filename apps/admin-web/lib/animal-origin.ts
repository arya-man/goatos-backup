/**
 * The three origin cohorts every origin filter and chart on the console uses (maintainer decision
 * 2026-09-26). The backend owns the rule (platform/animalorigin) and the labels (the page
 * contract's `view.origin.<key>` copy); this file only lists the wire keys in display order and
 * reads a URL value, so a bookmark from the two-way filter keeps working.
 */
export const ORIGIN_KEYS = ["farm_born", "procured_no_load", "procured_load"] as const;

export type OriginKey = (typeof ORIGIN_KEYS)[number];

/** Reads an `origin` URL value. The retired `purchased` meant "on a load", so it maps there. */
export function originFromParam(raw: string | undefined | null): OriginKey | "" {
  const value = (raw ?? "").trim().toLowerCase();
  if (value === "purchased") return "procured_load";
  return (ORIGIN_KEYS as readonly string[]).includes(value) ? (value as OriginKey) : "";
}

/**
 * The URL a page should redirect to when it was opened with the retired `origin=purchased`, so
 * the address bar, the filter control and the figures all say Procured (load). Null when the URL
 * is already canonical.
 */
export function canonicalOriginRedirect(
  path: string,
  params: Record<string, string | string[] | undefined>,
): string | null {
  const raw = params.origin;
  const value = (Array.isArray(raw) ? raw[0] : raw)?.trim().toLowerCase();
  if (value !== "purchased") return null;
  const next = new URLSearchParams();
  for (const [key, entry] of Object.entries(params)) {
    if (entry === undefined) continue;
    for (const one of Array.isArray(entry) ? entry : [entry]) next.append(key, one);
  }
  next.set("origin", "procured_load");
  return `${path}?${next.toString()}`;
}
