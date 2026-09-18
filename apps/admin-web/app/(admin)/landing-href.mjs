const WINDOW_FROM_PARAM = "wt_from";
const WINDOW_TO_PARAM = "wt_to";

function firstParam(params, key) {
  const value = params?.[key];
  return Array.isArray(value) ? value[0] : value;
}

export function hrefWithWindow(href, params, from, to) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params ?? {})) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  const park = firstParam(params, "park");
  const mode = park && park !== "all" ? "park" : firstParam(params, "scope_mode") === "park" ? "park" : "company";
  next.set("scope_mode", mode);
  if (mode !== "park" || !park || park === "all") next.delete("park");
  next.set(WINDOW_FROM_PARAM, from);
  next.set(WINDOW_TO_PARAM, to);
  return `${href}?${next.toString()}`;
}
