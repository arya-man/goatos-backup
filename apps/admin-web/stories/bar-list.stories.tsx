import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { BarList } from "@/components/bar-list";
import { useTheme } from "@mui/material/styles";
import { chartColors } from "@/components/app/chart-colors";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

const meta = {
  title: "Kit/BarList",
  component: BarList,
  decorators: [(S) => <Frame width={760}>{S()}</Frame>],
} satisfies Meta<typeof BarList>;
export default meta;
type Story = StoryObj<typeof meta>;

const RANKED = [
  { key: "castro-1", label: "CBE Castro 1", value: 31.4, display: "31.4 kg" },
  { key: "castro-2", label: "CBE Castro 2", value: 28.9, display: "28.9 kg" },
  { key: "godel-1", label: "CPT Godel 1 - Part 1", value: 24.2, display: "24.2 kg" },
  { key: "yashoda", label: "CPT Yashoda", value: 19.7, display: "19.7 kg" },
  { key: "f6", label: "CPT Fattening F6", value: 0, display: "0 kg" },
];

const GAIN = [
  { key: "f1", label: "CBE Fattening F1", value: 199, display: "199 g" },
  { key: "f3", label: "CBE Fattening F3", value: 183, display: "183 g" },
  { key: "f4", label: "CPT Fattening F4", value: -42, display: "−42 g" },
  { key: "f5", label: "CPT Fattening F5", value: -118, display: "−118 g" },
];

const FCR = [
  { key: "castro", label: "CBE Castro 1", value: 5.2, display: "5.2" },
  { key: "godel", label: "CPT Godel 2 - Part 1", value: 7.8, display: "7.8" },
  { key: "yashoda", label: "CPT Yashoda", value: 9.4, display: "9.4" },
];

const LONG = [
  {
    key: "long-1",
    label: "142 · Gokul Agronomics (42d) (CBE Castro 1, CBE Castro 2 +2)",
    value: 172,
    display: "172 g",
    note: "Per animal",
  },
  { key: "long-2", label: "Tanjung Karang Goat Supply Cooperative (Selangor) · Sojat · male", value: 214.3, display: "214.3 g" },
  { key: "long-3", label: "143 · Goat World (35d) (CPT Fattening F4, CPT Fattening F4B)", value: 230, display: "230 g" },
];

/** Grouped rows with a series legend; the series hue comes from the theme palette. */
function GroupedGainBands() {
  const primary = chartColors(useTheme())[0];
  return (
    <BarList
      ariaLabel="Gain bands by breed"
      valueNoun="Share"
      legend={[
        { label: "Above 250 g/day", color: primary },
        { label: "180 g/day or less", color: "var(--gain-under)" },
      ]}
      groups={[
        {
          key: "beetal",
          heading: "Beetal · 31 male kids",
          rows: [
            { key: "b-hi", label: "Above 250 g/day", value: 12.9, display: "12.9% (4)", color: primary },
            { key: "b-under", label: "180 g/day or less", value: 71, display: "71.0% (22)", color: "var(--gain-under)" },
          ],
        },
        {
          key: "sojat",
          heading: "Sojat · 29 male kids",
          rows: [
            { key: "s-hi", label: "Above 250 g/day", value: 20.7, display: "20.7% (6)", color: primary },
            { key: "s-under", label: "180 g/day or less", value: 24.1, display: "24.1% (7)", color: "var(--gain-under)" },
          ],
        },
      ]}
    />
  );
}

export const Default: Story = {
  args: { ariaLabel: "Average weight by pen", rows: RANKED, valueNoun: "Average weight" },
};

export const AllStates: Story = {
  args: { ariaLabel: "Bar list states" },
  render: () => (
    <States>
      <StateBlock label="Ranked list, one measure, a measured 0 on an empty track">
        <BarList ariaLabel="Average weight by pen" rows={RANKED} valueNoun="Average weight" />
      </StateBlock>
      <StateBlock label="Loss track: a series straddling zero draws a zero rule, losses grow left in the danger tone">
        <BarList ariaLabel="Daily gain by pen" rows={GAIN} valueNoun="Daily gain" />
      </StateBlock>
      <StateBlock label="Reference track: dashed break-even mark on every row">
        <BarList ariaLabel="FCR by pen" rows={FCR} valueNoun="FCR" reference={{ value: 7, label: "Break-even FCR 7" }} wide />
      </StateBlock>
      <StateBlock label="Long labels and a note chip wrap inside the card">
        <BarList ariaLabel="Daily gain by load" rows={LONG} valueNoun="Daily gain" />
      </StateBlock>
      <StateBlock label="Groups with a series legend">
        <GroupedGainBands />
      </StateBlock>
      <StateBlock label="Empty">
        <BarList ariaLabel="Average weight by pen" rows={[]} emptyLabel="No pen weighed in this window" />
      </StateBlock>
    </States>
  ),
};

export const InCard: Story = {
  args: { ariaLabel: "Daily gain by load" },
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Daily gain by load" />
      <BarList ariaLabel="Daily gain by load" rows={LONG} valueNoun="Daily gain" />
    </Card>
  ),
};

export const Mobile: Story = {
  args: { ariaLabel: "Bar list at phone width" },
  globals: MOBILE,
  render: () => (
    <States>
      <StateBlock label="Loss track">
        <BarList ariaLabel="Daily gain by pen" rows={GAIN} valueNoun="Daily gain" />
      </StateBlock>
      <StateBlock label="Reference track">
        <BarList ariaLabel="FCR by pen" rows={FCR} valueNoun="FCR" reference={{ value: 7, label: "Break-even FCR 7" }} wide />
      </StateBlock>
      <StateBlock label="Long labels">
        <BarList ariaLabel="Daily gain by load" rows={LONG} valueNoun="Daily gain" />
      </StateBlock>
    </States>
  ),
};
