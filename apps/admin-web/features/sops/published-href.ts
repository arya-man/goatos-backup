// The library URL an editor returns to after Publish, carrying WHICH SOP and WHICH version just
// went live so the library can say so in a banner and light up that card. Query-only: the
// library is a server-rendered route, so this is a real navigation away from the editor (that is
// the point -- the editor closes), not a same-page overlay.
export function publishedHref(basePath: string, sopId: string, versionNumber?: number): string {
  const params = new URLSearchParams({ published: sopId });
  if (versionNumber !== undefined && Number.isFinite(versionNumber)) params.set("v", String(versionNumber));
  return `${basePath}?${params.toString()}`;
}

/** Parses the banner state back out of the library's search params. */
export function publishedFromSearch(sp: Record<string, string | string[] | undefined>): { sopId: string; version: number | null } | null {
  const raw = sp.published;
  const sopId = (Array.isArray(raw) ? raw[0] : raw)?.trim();
  if (!sopId) return null;
  const v = sp.v;
  const n = Number.parseInt((Array.isArray(v) ? v[0] : v) ?? "", 10);
  return { sopId, version: Number.isFinite(n) && n > 0 ? n : null };
}
