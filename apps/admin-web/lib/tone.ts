export type KitTone = "primary" | "info" | "success" | "warning" | "error" | "violet" | "neutral";

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

/** CSS variable triple for a tone: solid, soft tint background, ink (text on soft). */
export function toneVars(tone: KitTone): { solid: string; soft: string; ink: string } {
  if (tone === "neutral") return { solid: "var(--fg)", soft: "rgb(var(--g500-rgb)/.16)", ink: "var(--fg-muted)" };
  return { solid: `var(--${tone})`, soft: `var(--${tone}-soft)`, ink: `var(--${tone}-ink)` };
}
