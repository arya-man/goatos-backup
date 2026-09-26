import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { GroupedColumns, type GroupedDatum, type GroupedSeries } from "@/components/grouped-columns";
import { SvgBars, SvgStackedBars } from "@/components/svg-bars";
import { SvgColumnBars } from "@/components/svg-column-bars";
import { MonthlyColumnsCard, SeriesLines, SeriesPie, StackedColumns } from "@/components/svg-series";
import { HistoryChart } from "@/features/herd-signals/herd-signals-history-chart";
import type { HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

// The page charts drawn with the template's ApexCharts Chart (components/minimal/chart). One story
// per chart family, real states, plus a 390px variant of each.

const meta: Meta = {
  title: "Charts/ApexCharts",
  decorators: [(S) => <Frame width={1100}>{S()}</Frame>],
};
export default meta;
type Story = StoryObj;

const DAYS = Array.from({ length: 30 }, (_, i) => {
  const d = new Date(Date.UTC(2026, 7, 28 + i));
  return d.toISOString().slice(0, 10);
});

const BREEDS = [
  { key: "beetal", label: "Beetal", value: 471 },
  { key: "anantapur-sheep", label: "Anantapur Sheep", value: 350 },
  { key: "sojat", label: "Sojat", value: 212 },
  { key: "anantapur", label: "Anantapur", value: 189 },
  { key: "sirohi", label: "Sirohi", value: 65 },
  { key: "malai", label: "Malai", value: 48 },
  { key: "osmanabadi", label: "Osmanabadi", value: 40 },
  { key: "boer-x-malai-long", label: "Boer x Malai cross from the Coimbatore procurement load", value: 1 },
];

const LOAD_SERIES: GroupedSeries[] = [
  { key: "purchased", label: "Purchased", tone: "info" },
  { key: "sold", label: "Sold", tone: "ok" },
  { key: "mortality", label: "Mortality", tone: "danger" },
  { key: "remaining", label: "Remaining", tone: "warn" },
];
const load = (n: number, pens: string, vendor: string, v: (number | null)[]): GroupedDatum => ({
  key: String(n),
  axisLabel: `${n} (${pens})`,
  label: `Load ${n} · ${vendor} (${pens})`,
  subLabel: vendor,
  values: v,
  displays: v.map((x) => (x === null ? "Not recorded" : x.toLocaleString("en-IN"))),
});
const LOADS = [
  load(146, "Goat World", "Goat World", [12, 11, 1, 0]),
  load(145, "CBE Fattening F3", "Rajasthan Farms", [15, 0, 0, 15]),
  load(143, "CPT Fattening F4, CPT Fattening F4B", "Goat World", [22, 6, 1, 15]),
  load(142, "CBE Castro 2", "Gokul Agronomics", [16, 0, 0, 16]),
  load(141, "CBE Fattening F1", "Rajasthan Farms", [18, 6, 1, null]),
];

// Money per load: three close figures per load (the collision case in judge 4 P1-3) on a ₹ axis.
const inrShort = (v: number) => (v >= 100000 ? `₹${(v / 100000).toFixed(1).replace(/\.0$/, "")}L` : `₹${(v / 1000).toFixed(1).replace(/\.0$/, "")}k`);
const MONEY_SERIES: GroupedSeries[] = [
  { key: "purchase", label: "Purchase value", tone: "info" },
  { key: "sold", label: "Sold value", tone: "ok" },
  { key: "profit", label: "Profit / loss", tone: "warn" },
];
const money = (n: number, sold: string, v: (number | null)[]): GroupedDatum => ({
  key: String(n),
  axisLabel: `${n} (CPT Fattening F4)`,
  label: `Load ${n}`,
  subLabel: sold,
  values: v,
  displays: v.map((x, i) => (x === null ? "Cost not recorded" : `${i === 2 ? "+" : ""}${inrShort(x)}`)),
});
const MONEY_LOADS = [
  money(143, "4 / 20 sold", [198000, 63700, 88700]),
  money(141, "6 / 18 sold", [187600, 100000, 110000]),
  money(146, "11 / 12 sold", [130000, 213000, 83200]),
];
// Month by month: every month labelled with its figure, a measured 0 on the baseline.
const MONTHS = ["Mar 2026", "Apr 2026", "May 2026", "Jun 2026", "Jul 2026", "Aug 2026", "Sep 2026"];
const MONTH_KEYS = ["2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09"];
const MANURE = [47000, 95000, 27000, 26000, 14000, 10000, 0];

const feedDays = DAYS.map((day, i) => ({
  key: day,
  label: day,
  segments: [2400 + (i % 5) * 40, 1800 + (i % 3) * 60, 620, 410, i === 27 ? 0 : 260],
}));
const lineSeries = [
  { label: "Spend", colorVar: "var(--primary)", points: DAYS.map((_, i) => (i === 26 ? null : 8600 + Math.round(Math.sin(i / 3) * 500))) },
];
const kgSeries = { label: "Kg fed", colorVar: "var(--primary)", points: DAYS.map((_, i) => 2300 + Math.round(Math.cos(i / 4) * 90)) };

const buckets: HerdSignalTimelineBucket[] = Array.from({ length: 96 }, (_, i) => {
  const start = new Date(Date.UTC(2026, 8, 25, 0, 0) + i * 300_000).toISOString();
  const gap = i >= 40 && i < 46;
  return {
    bucket_start: start,
    bucket_seconds: 300,
    first_motion_count: gap ? null : 1000 + i * 10,
    last_motion_count: gap ? null : 1010 + i * 10,
    motion_delta: gap ? null : i === 46 ? 380 : i % 17 === 0 ? 0 : 40 + ((i * 37) % 160),
    packet_count: gap ? 0 : 5,
    avg_rssi_dbm: gap ? null : -71,
    min_rssi_dbm: gap ? null : -80,
    max_rssi_dbm: gap ? null : -62,
    is_gap: gap,
    gap_delta: i === 46,
  };
});

function All() {
  return (
    <States>
      <StateBlock label="SvgBars (AnalyticsConversionRates): ranked, share, long label, empty">
        <Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Count by breed" /><SvgBars data={BREEDS} valueNoun="animals" chartLabel="Count by breed" emptyLabel="Nothing recorded" showShare maxBars={8} /></Card>
        <Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Empty" /><SvgBars data={[]} valueNoun="animals" chartLabel="Empty" emptyLabel="Nothing recorded in this scope yet." /></Card>
      </StateBlock>
      <StateBlock label="SvgStackedBars (stage × sex)">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Count by stage and sex" />
          <SvgStackedBars
            valueNoun="animals"
            chartLabel="Count by stage and sex"
            emptyLabel="Nothing recorded"
            data={[
              { key: "adult", label: "Adult", total: 1218, segments: [{ key: "f", label: "Female", value: 970, colorVar: "var(--info)" }, { key: "m", label: "Male", value: 248, colorVar: "var(--amber)" }] },
              { key: "md", label: "Milk drinking", total: 98, segments: [{ key: "f", label: "Female", value: 10, colorVar: "var(--info)" }, { key: "m", label: "Male", value: 88, colorVar: "var(--amber)" }] },
              { key: "doe", label: "Doe", total: 40, segments: [{ key: "f", label: "Female", value: 40, colorVar: "var(--info)" }, { key: "m", label: "Male", value: 0, colorVar: "var(--amber)" }] },
            ]}
          />
        </Card>
      </StateBlock>
      <StateBlock label="SvgColumnBars (AnalyticsWebsiteVisits): 14-day trend with comparison, a zero day">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Reviews per day" />
          <SvgColumnBars
            chartLabel="Reviews per day"
            valueNoun="Reviewed"
            compareNoun="Rejected"
            emptyLabel="No reviews"
            data={DAYS.slice(0, 14).map((d, i) => ({ key: d, label: d.split("-").reverse().join("/"), value: i === 5 ? 0 : 20 + (i % 4) * 6, compareValue: i % 3 }))}
          />
        </Card>
      </StateBlock>
      <StateBlock label="GroupedColumns: grouped loads, figures in the tooltip, absent value blank">
        <Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Animals per load" /><GroupedColumns series={LOAD_SERIES} data={LOADS} chartLabel="Animals per load" emptyLabel="No loads" /></Card>
        <Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Money per load" /><GroupedColumns series={MONEY_SERIES} data={MONEY_LOADS} money chartLabel="Money per load" emptyLabel="No loads" /></Card>
        <Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Empty" /><GroupedColumns series={LOAD_SERIES} data={[]} chartLabel="Empty" emptyLabel="No loads in this window" /></Card>
      </StateBlock>
      <StateBlock label="MonthlyColumnsCard: template chart card, short months, year select, legend total">
        <MonthlyColumnsCard
          title="Manure sold by month"
          months={MONTHS.map((m, i) => ({ key: MONTH_KEYS[i], label: m, segments: [MANURE[i]], extra: [{ label: "Manure revenue", value: `₹${(MANURE[i] * 1.6).toLocaleString("en-IN")}` }] }))}
          seriesLabels={["Manure (kg)"]}
          valueNoun="kg"
          chartLabel="Manure sold by month"
          emptyLabel="No manure sold"
        />
      </StateBlock>
      <StateBlock label="StackedColumns (AppAreaInstalled): 30 days, last day labelled">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Daily directed feed" />
          <StackedColumns days={feedDays} seriesLabels={["Hybrid", "COFS", "Dry Masoor Bhusa", "Hedge Lucerne", "UHT Milk"]} valueNoun="kg" chartLabel="Daily directed feed" emptyLabel="No feed" hideZeroInTip />
        </Card>
      </StateBlock>
      <StateBlock label="SeriesLines (EcommerceYearlySales) with dashed secondary axis, a missing day">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Hybrid" />
          <SeriesLines series={lineSeries} secondary={{ series: kgSeries, valueNoun: "kg / day" }} dayLabels={DAYS} valueNoun="₹ / day" chartLabel="Hybrid" emptyLabel="No spend" />
        </Card>
      </StateBlock>
      <StateBlock label="SeriesPie (AppCurrentDownload donut)">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Feed spend share" />
          <SeriesPie
            valueNoun="/ day"
            chartLabel="Feed spend share"
            emptyLabel="No spend"
            slices={[
              { label: "Hybrid", value: 8767, colorVar: "var(--brand)" },
              { label: "COFS", value: 6866, colorVar: "var(--info)" },
              { label: "Dry Masoor Bhusa", value: 5067, colorVar: "var(--amber)" },
              { label: "Hedge Lucerne", value: 4503, colorVar: "var(--purple)" },
              { label: "RGS Concentrate", value: 12, colorVar: "var(--teal)" },
            ]}
          />
        </Card>
      </StateBlock>
      <StateBlock label="HistoryChart (herd signals): gap band, reconnect total, zero deltas, baseline">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Motion history" />
          <HistoryChart buckets={buckets} baseline={90} height={240} markers={[{ atMs: Date.parse(buckets[20].bucket_start) + 90_000, color: "var(--info)", label: "Vaccination" }]} />
        </Card>
      </StateBlock>
    </States>
  );
}

export const AllCharts: Story = { render: () => <All /> };
export const MobileAllCharts: Story = { render: () => <All />, globals: MOBILE };
