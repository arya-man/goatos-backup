/**
 * The pager's range noun for `total` rows: "1 row", "25 rows". A noun the contract already serves
 * plural ("Workflows") is used as it is, never doubled ("1–10 Workflowss", J3B P2-3; test
 * `pager-noun-plural` in pager-noun.test.mjs).
 */
export function pagerNoun(noun: string, total: number): string {
  if (total === 1 || /s$/i.test(noun)) return noun;
  return `${noun}s`;
}

