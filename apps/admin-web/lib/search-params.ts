export type RouteSearchParams = Record<string, string | string[] | undefined>;

const MAX_CURSOR_STACK_DEPTH = 8;

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
  return hrefWithPagedCursor(pathname, params, "cursor", cursor, "page", "cursor_stack");
}

export function hrefWithoutCursor(pathname: string, params: RouteSearchParams) {
  return hrefWithoutPagedCursor(pathname, params, "cursor", "page", "cursor_stack");
}

export function hrefPreviousCursor(pathname: string, params: RouteSearchParams) {
  return hrefPreviousPagedCursor(pathname, params, "cursor", "page", "cursor_stack");
}

export function hrefWithPagedCursor(
  pathname: string,
  params: RouteSearchParams,
  cursorKey: string,
  cursor: string | null,
  pageKey = "page",
  stackKey = `${cursorKey}_stack`,
) {
  if (!cursor) return null;
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === cursorKey || key === pageKey || key === stackKey) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  const stack = all(params, stackKey);
  const currentCursor = one(params, cursorKey);
  if (currentCursor) stack.push(currentCursor);
  for (const item of stack.slice(-MAX_CURSOR_STACK_DEPTH)) next.append(stackKey, item);
  const currentPage = boundedInt(one(params, pageKey), 1, 1, 1000000);
  next.set(cursorKey, cursor);
  next.set(pageKey, String(currentPage + 1));
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export function hrefPreviousPagedCursor(
  pathname: string,
  params: RouteSearchParams,
  cursorKey: string,
  pageKey = "page",
  stackKey = `${cursorKey}_stack`,
) {
  const currentPage = boundedInt(one(params, pageKey), 1, 1, 1000000);
  if (currentPage <= 1) return null;
  const stack = all(params, stackKey);
  const previousCursor = stack.pop();
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === cursorKey || key === pageKey || key === stackKey) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const item of stack) next.append(stackKey, item);
  if (previousCursor) next.set(cursorKey, previousCursor);
  if (previousCursor && currentPage > 2) next.set(pageKey, String(currentPage - 1));
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export function hrefWithoutPagedCursor(pathname: string, params: RouteSearchParams, cursorKey: string, pageKey = "page", stackKey = `${cursorKey}_stack`) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === cursorKey || key === pageKey || key === stackKey) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function all(params: RouteSearchParams, key: string): string[] {
  const value = params[key];
  if (!value) return [];
  return Array.isArray(value) ? value.filter(Boolean) : [value];
}

export function hrefWithParam(pathname: string, params: RouteSearchParams, key: string, value: string | null) {
  if (!value) return null;
  const next = new URLSearchParams();
  for (const [name, currentValue] of Object.entries(params)) {
    if (name === "cursor" || name === key) continue;
    if (Array.isArray(currentValue)) {
      for (const item of currentValue) next.append(name, item);
    } else if (currentValue) {
      next.set(name, currentValue);
    }
  }
  next.set(key, value);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export function hrefWithoutAction(pathname: string, params: RouteSearchParams) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "action_status" || key === "action_message") continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}
