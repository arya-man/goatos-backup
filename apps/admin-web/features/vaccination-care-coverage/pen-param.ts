// The Pen multi-select's page URL value. The ticked pens travel as ONE parameter, comma-joined,
// each value URI-encoded first so a comma inside a pen value can never split it.
//
// Why not repeat the key (?cc_pen=a&cc_pen=b): the Next.js router identified the page by its
// search params with only the LAST value of a repeated key, so going from [a, b] to [b] looked
// like "the same page" and it kept the old table on screen while the URL changed underneath it.
// One parameter makes every distinct selection a distinct page.
//
// Shared by the server board and the client filter bar; it is a plain module (no "use client"),
// so both sides import the real functions rather than a client reference.
export const PENS_PARAM = "cc_pens";

export function encodePens(pens: readonly string[]): string {
  return pens.map((pen) => encodeURIComponent(pen)).join(",");
}

export function decodePens(raw: string | undefined | null): string[] {
  if (!raw) return [];
  const out: string[] = [];
  for (const part of raw.split(",")) {
    if (!part) continue;
    try {
      out.push(decodeURIComponent(part));
    } catch {
      // A hand-edited URL with a broken escape drops that one value rather than the whole filter.
    }
  }
  return out;
}
