/**
 * Realistic Goat OS fixture data for stories.
 *
 * Everything here is farm content — pens, parks, sheds, kids, ADG, vendors,
 * loads, vaccination rounds — never lorem ipsum, so a story reads like the real
 * screen and overflow/tabular-number problems show up honestly.
 */

/** Weekly ADG (g/day) per park, plus the target line. */
export const adgWeeks = [
  { week: "W23", godel1: 132, godel2: 118, godel3: 141, target: 130 },
  { week: "W24", godel1: 139, godel2: 124, godel3: 136, target: 130 },
  { week: "W25", godel1: 128, godel2: 131, godel3: 147, target: 130 },
  { week: "W26", godel1: 145, godel2: 127, godel3: 152, target: 130 },
  { week: "W27", godel1: 151, godel2: 136, godel3: 149, target: 130 },
  { week: "W28", godel1: 147, godel2: 142, godel3: 158, target: 130 },
  { week: "W29", godel1: 156, godel2: 138, godel3: 161, target: 130 },
  { week: "W30", godel1: 162, godel2: 145, godel3: 157, target: 130 },
];

/** Milk feeding litres by shed, stacked by feed type. */
export const feedByShed = [
  { shed: "Godel 1 - Part 1", whole: 184, replacer: 96, creep: 42 },
  { shed: "Godel 1 - Part 2", whole: 162, replacer: 108, creep: 38 },
  { shed: "Godel 2 - Part 1", whole: 141, replacer: 87, creep: 55 },
  { shed: "Godel 2 - Part 3", whole: 127, replacer: 74, creep: 61 },
  { shed: "Godel 3 - Part 1", whole: 198, replacer: 112, creep: 33 },
  { shed: "Quarantine pen A", whole: 46, replacer: 51, creep: 8 },
];

/** Long shed labels: the axis-overflow case at 390px. */
export const feedByShedLongLabels = feedByShed.map((row, i) => ({
  ...row,
  shed: `${row.shed} — intake batch ${2411 + i}`,
}));

/** Kid intake vs mortality, month by month. */
export const intakeMonths = [
  { month: "Jan", intake: 420, weaned: 386, mortality: 14 },
  { month: "Feb", intake: 512, weaned: 470, mortality: 21 },
  { month: "Mar", intake: 468, weaned: 441, mortality: 12 },
  { month: "Apr", intake: 604, weaned: 559, mortality: 26 },
  { month: "May", intake: 588, weaned: 552, mortality: 18 },
  { month: "Jun", intake: 651, weaned: 611, mortality: 23 },
  { month: "Jul", intake: 712, weaned: 668, mortality: 29 },
  { month: "Aug", intake: 689, weaned: 651, mortality: 17 },
  { month: "Sep", intake: 734, weaned: 697, mortality: 20 },
  { month: "Oct", intake: 802, weaned: 755, mortality: 25 },
  { month: "Nov", intake: 771, weaned: 729, mortality: 19 },
  { month: "Dec", intake: 845, weaned: 796, mortality: 31 },
];

/** Head count by weight band (donut). */
export const weightBands = [
  { label: "Under 12 kg", value: 184 },
  { label: "12–18 kg", value: 612 },
  { label: "18–24 kg", value: 948 },
  { label: "24–30 kg", value: 431 },
  { label: "Over 30 kg", value: 127 },
];

export const weightBandsLongLabels = [
  { label: "Under 12 kg — holdback, re-weigh in 7 days", value: 184 },
  { label: "12–18 kg — on feed plan, no action", value: 612 },
  { label: "18–24 kg — sale-ready pending vet clearance", value: 948 },
];

/** Procurement vendors and their delivered loads. */
export const vendorLoads = [
  { vendor: "Ambika Livestock", loads: 14, head: 612, rejected: 9 },
  { vendor: "Sri Venkateswara Traders", loads: 11, head: 489, rejected: 21 },
  { vendor: "Karnataka Goat Collective", loads: 9, head: 377, rejected: 4 },
  { vendor: "Hosur Farm Supply", loads: 6, head: 241, rejected: 12 },
  { vendor: "Nellore Kid Suppliers", loads: 4, head: 158, rejected: 2 },
];

/** Rows for the table/pagination stories. */
export type PenRow = {
  tag: string;
  shed: string;
  vendor: string;
  weightKg: number;
  adg: number;
  status: "Healthy" | "Watch" | "Treatment" | "Quarantine";
};

const sheds = ["Godel 1 - Part 1", "Godel 1 - Part 2", "Godel 2 - Part 1", "Godel 2 - Part 3", "Godel 3 - Part 1"];
const statuses: PenRow["status"][] = ["Healthy", "Healthy", "Watch", "Treatment", "Quarantine"];

export const penRows: PenRow[] = Array.from({ length: 137 }, (_, i) => ({
  tag: `SF-${String(48 + i * 3).padStart(4, "0")}`,
  shed: sheds[i % sheds.length],
  vendor: vendorLoads[i % vendorLoads.length].vendor,
  weightKg: Number((11.4 + ((i * 7) % 190) / 10).toFixed(1)),
  adg: 96 + ((i * 13) % 84),
  status: statuses[i % statuses.length],
}));

export const kg = (v: number) => `${v.toLocaleString()} kg`;
export const gPerDay = (v: number) => `${v} g`;
export const head = (v: number) => `${v.toLocaleString()} head`;
export const litres = (v: number) => `${v.toLocaleString()} L`;
