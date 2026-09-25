// Two board-count rules that read only plain data, kept free of app imports so they are tested
// directly (work-board-counts.test.mjs).

export type BoardCounts = {
  total: number;
  by_lane: Record<string, number>;
  by_module: Record<string, number>;
};

export type LoadedCard = { parkKey: string; lane: string; module: string };

// A module whose COUNT timed out on the server is left out of the column totals, while its cards
// may still load beneath them -- a heading could read 12 over 15 cards. The maintainer's rule
// (2026-09-25): "when it loads and the card comes, increase the number". So every loaded card of a
// module degraded IN ITS OWN PARK is added to its column, the module and the total. A module that
// counted fine is never added twice: its cards are already in the server's numbers.
export function withLoadedDegradedCards<S extends BoardCounts>(summary: S, cards: LoadedCard[], degradedByPark: Map<string, Set<string>>): S {
  const extra = cards.filter((card) => degradedByPark.get(card.parkKey)?.has(card.module));
  if (extra.length === 0) return summary;
  const byLane = { ...summary.by_lane };
  const byModule = { ...summary.by_module };
  for (const card of extra) {
    byLane[card.lane] = (byLane[card.lane] ?? 0) + 1;
    byModule[card.module] = (byModule[card.module] ?? 0) + 1;
  }
  return { ...summary, total: summary.total + extra.length, by_lane: byLane, by_module: byModule };
}

// The Module menu lists a module only when it has work on the day (maintainer, 2026-09-25: hide
// the modules that have no task), the same rule the phone's chips follow. A module whose count
// timed out is kept (its work is unknown, not absent), and so is any module the reader selected,
// so a chosen module can always be seen and cleared.
export function modulesWithWork<O extends { key: string }>(options: O[], byModule: Record<string, number>, degraded: Set<string>, selected: string[]): O[] {
  return options.filter((option) => (byModule[option.key] ?? 0) > 0 || degraded.has(option.key) || selected.includes(option.key));
}
