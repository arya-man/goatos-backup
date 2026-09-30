// Pure URL helper shared by the discipline pages (kept import-free for node --test).
export function hrefWith(pathname: string, sp: Record<string, string | string[] | undefined>, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    if (value === undefined || key in patch) continue;
    for (const v of Array.isArray(value) ? value : [value]) query.append(key, v);
  }
  for (const [key, value] of Object.entries(patch)) if (value) query.set(key, value);
  const qs = query.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}
