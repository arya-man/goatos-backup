// Missing ration rates, read off tomorrow's feed sheet preview (maintainer request 2026-09-24).
//
// Only the no-ration-rate reason is listed: the other block reasons (an unknown pen tag or breed,
// no session template) are not fixed by adding a rate on Feed Config.

export type MissingRate = { rationGroup: string; shedTag: string; feedItem: string; pens: string[] };

type PreviewRow = {
  ration_group: string;
  shed_tag: string;
  shed_label: string;
  partition_label?: string | null;
  items?: { feed_item: string; status: string; blocked_reason?: { code?: string } | null }[] | null;
};

/** One entry per (ration group, pen tag, feed item) the sheet blocks, with every pen it blocks. */
export function groupMissingRates<T extends PreviewRow>(rows: readonly T[], penName: (row: T) => string): MissingRate[] {
  const byKey = new Map<string, { gap: Omit<MissingRate, "pens">; pens: Set<string> }>();
  for (const row of rows) {
    for (const item of row.items ?? []) {
      if (item.status !== "blocked" || item.blocked_reason?.code !== "no_ration_rate") continue;
      const key = `${row.ration_group}\u0000${row.shed_tag}\u0000${item.feed_item}`;
      const entry = byKey.get(key) ?? {
        gap: { rationGroup: row.ration_group, shedTag: row.shed_tag, feedItem: item.feed_item },
        pens: new Set<string>(),
      };
      entry.pens.add(penName(row));
      byKey.set(key, entry);
    }
  }
  return [...byKey.values()]
    .map(({ gap, pens }) => ({ ...gap, pens: [...pens].sort((a, b) => a.localeCompare(b, undefined, { numeric: true })) }))
    .sort(
      (a, b) =>
        a.rationGroup.localeCompare(b.rationGroup) ||
        a.shedTag.localeCompare(b.shedTag) ||
        a.feedItem.localeCompare(b.feedItem),
    );
}
