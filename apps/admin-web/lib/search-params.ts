export type RouteSearchParams = Record<string, string | string[] | undefined>;

export function one(params: RouteSearchParams, key: string): string | undefined {
  const value = params[key];
  return Array.isArray(value) ? value[0] : value;
}

export function boundedInt(value: string | undefined, fallback: number, min: number, max: number): number {
  if (!value) return fallback;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.min(Math.max(parsed, min), max);
}

export function hrefWithCursor(pathname: string, params: RouteSearchParams, cursor: string | null) {
  return hrefWithParam(pathname, params, "cursor", cursor);
}

export function hrefWithParam(pathname: string, params: RouteSearchParams, key: string, value: string | null) {
  if (!value) return null;
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "cursor") continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  next.delete(key);
  if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}
