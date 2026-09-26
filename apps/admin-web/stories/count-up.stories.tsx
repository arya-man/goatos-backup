import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { CountUp } from "@/components/app/count-up";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

// The visual lane drives Storybook through Playwright (navigator.webdriver): it gets the settled
// value so the snapshot is deterministic; a person browsing Storybook still sees the 2.4 s count.
const SLOW_MS = typeof navigator !== "undefined" && navigator.webdriver ? 0 : 2400;

const meta = {
  title: "Kit/CountUp",
  component: CountUp,
  decorators: [(S) => <Frame width={720}>{S()}</Frame>],
} satisfies Meta<typeof CountUp>;
export default meta;
export type Story = StoryObj<typeof meta>;

const Big = ({ children }: { children: React.ReactNode }) => (
  <div style={{ font: "800 34px/1.1 var(--font-sans)", color: "var(--fg)" }}>{children}</div>
);

export const Default: Story = { args: { value: 1248 }, render: (a) => <Big><CountUp {...a} /></Big> };

export const AllStates: Story = {
  args: { value: 1248 },
  render: () => (
    <States>
      <StateBlock label="Magnitudes — head, kids, ADG, herd">
        <Big><CountUp value={0} /></Big>
        <Big><CountUp value={9} /></Big>
        <Big><CountUp value={412} /></Big>
        <Big><CountUp value={1248} /></Big>
        <Big><CountUp value={18420} /></Big>
      </StateBlock>
      <StateBlock label="Decimals — mortality %, avg weight">
        <Big><CountUp value={1.4} digits={1} />%</Big>
        <Big><CountUp value={24.63} digits={2} /> kg</Big>
      </StateBlock>
      <StateBlock label="Negative and custom format">
        <Big><CountUp value={-18} /> head</Big>
        <Big><CountUp value={182450} format={(n) => `S$${Math.round(n).toLocaleString()}`} /></Big>
        <Big><CountUp value={182} format={(n) => `${Math.round(n)} g/day`} /></Big>
      </StateBlock>
      <StateBlock label="Duration — instant (0 ms) vs slow (2.4 s)">
        <Big><CountUp value={520} duration={0} /></Big>
        <Big><CountUp value={520} duration={SLOW_MS} /></Big>
      </StateBlock>
      <StateBlock label="Non-finite guard (NaN renders as NaN-safe text, no crash)">
        <Big><CountUp value={Number.NaN} /></Big>
      </StateBlock>
    </States>
  ),
  // Capture the SETTLED frame: the slow 2.4 s counter is otherwise snapshotted mid-count and the
  // baseline flakes. The play waits for both 520 counters to land; app behaviour is unchanged.
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    await waitFor(() => expect(c.getAllByText("520")).toHaveLength(2), { timeout: 5000 });
  },
};

/** Behaviour: changing the value re-animates to the new number. */
export const UpdatesOnChange: Story = {
  args: { value: 1248 },
  render: function Render() {
    const [v, setV] = React.useState(1248);
    return (
      <Card sx={{ p: { xs: 2, sm: 3 } }}>
        <CardHeader sx={{ p: 0, mb: 2 }} title="Head weighed today" subheader="Press the button to post a new weighing batch" />
        <Big><CountUp value={v} /></Big>
        <Button type="button" variant="outlined" color="inherit" size="small" sx={{ mt: 1.5 }} onClick={() => setV(1310)}>
          Post batch
        </Button>
      </Card>
    );
  },
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    await userEvent.click(c.getByRole("button", { name: "Post batch" }));
    await expect(await c.findByText("1,310", undefined, { timeout: 4000 })).toBeInTheDocument();
  },
};

export const Mobile: Story = {
  args: { value: 18420 },
  globals: MOBILE,
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Weighings this quarter" />
      <Big><CountUp value={18420} /></Big>
      <Big><CountUp value={24.63} digits={2} /> kg</Big>
    </Card>
  ),
};
