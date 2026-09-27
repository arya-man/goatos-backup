/**
 * TR1-#14: a routed page with no nav leaf of its own (the withheld Herd Register at /counts/herd, the
 * Weights deep link) still belongs to its module. When no nav entry matches the path, the group whose
 * enabled leaves ALL live under the path's first segment is the active one, so the sidebar and the
 * phone menu open and highlight it (the template opens and highlights the active group). A group with
 * mixed prefixes never claims a path this way. guard: nav-module-fallback (lib/nav-module-group.test.mjs)
 */
export function moduleGroupForPath(
  pathname: string,
  groups: ReadonlyArray<{ id: string; leaves: ReadonlyArray<{ href: string; enabled: boolean }> }>,
): string | null {
  const seg = pathname.split("/")[1];
  if (!seg) return null;
  for (const group of groups) {
    const leaves = group.leaves.filter((leaf) => leaf.enabled);
    if (!leaves.length) continue;
    if (leaves.every((leaf) => firstSegment(leaf.href) === seg)) return group.id;
  }
  return null;
}

function firstSegment(href: string): string {
  return (href.split(/[?#]/)[0] ?? "").split("/")[1] ?? "";
}
