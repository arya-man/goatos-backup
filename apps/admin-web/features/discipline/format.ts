// Pure display helpers for the discipline pages (import-free for node --test).

/** ₹1,500 / ₹1,00,000 -- the backend's RupeesLabel, for totals the page moves in place. */
export function rupeesLabel(amount: number): string {
  const neg = amount < 0;
  const s = String(Math.abs(Math.trunc(amount)));
  let out = s;
  if (s.length > 3) {
    let head = s.slice(0, -3);
    const tail = s.slice(-3);
    const groups: string[] = [];
    while (head.length > 2) {
      groups.unshift(head.slice(-2));
      head = head.slice(0, -2);
    }
    if (head) groups.unshift(head);
    out = `${groups.join(",")},${tail}`;
  }
  return `${neg ? "-" : ""}₹${out}`;
}

/** Today's IST date as YYYY-MM-DD (a wire value for the date field, never displayed). */
export function todayKey(now: Date = new Date()): string {
  const ist = new Date(now.getTime() + (5 * 60 + 30) * 60_000);
  return ist.toISOString().slice(0, 10);
}
