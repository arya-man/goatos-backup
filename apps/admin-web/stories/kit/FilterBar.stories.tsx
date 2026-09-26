import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { FilterBar } from "@/components/app/filter-bar";
import { DateRangeField } from "@/components/app/date-range-field";
import { Canvas, MOBILE, PARKS, PENS } from "../_data";
import { fmtDate } from "@/lib/format";

/** The one filter row: search · controls (MUI selects, date range) · right-aligned actions · summary. */
const meta: Meta<typeof FilterBar> = {
  title: "Kit/FilterBar",
  component: FilterBar,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <Canvas>
        <Story />
      </Canvas>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof meta>;

function Demo({ withDates = true }: { withDates?: boolean }) {
  const [q, setQ] = React.useState("");
  const [park, setPark] = React.useState("seletar");
  const [pen, setPen] = React.useState("");
  const [range, setRange] = React.useState({ from: "2026-09-01", to: "" });
  const active = [park && "Seletar Park", pen && PENS.find((p) => p.value === pen)?.label, range.from && `from ${fmtDate(range.from)}`].filter(Boolean);
  return (
    <FilterBar
      search={{ value: q, onChange: setQ, placeholder: "Search tag or pen" }}
      actions={
        <>
          <Button color="primary" variant="outlined">Export</Button>
          <Button color="primary" variant="contained">New load</Button>
        </>
      }
      summary={<span data-testid="summary">{`${(1_284 - q.length * 10).toLocaleString("en-IN")} results · ${active.join(" · ")}`}</span>}
    >
      <TextField
        select
        label="Park"
        value={park}
        onChange={(event) => setPark(event.target.value)}
        sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {PARKS.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        select
        label="Pen"
        value={pen}
        onChange={(event) => setPen(event.target.value)}
        sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {PENS.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
      {withDates ? <DateRangeField label="Dates" from={range.from} to={range.to} fromLabel="From" toLabel="To" onChange={setRange} /> : null}
    </FilterBar>
  );
}

export const Default: Story = {
  render: () => <Demo />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const search = c.getByRole("textbox", { name: "Search tag or pen" });
    await userEvent.type(search, "SF-048");
    await expect(search).toHaveValue("SF-048");
    await expect(c.getByTestId("summary")).toHaveTextContent("results");
  },
};

/** At 390px the controls wrap to full-width rows and actions sit below; nothing scrolls sideways. */
export const Mobile: Story = { render: () => <Demo />, ...MOBILE };

/** Search only: the lightest form, e.g. a people directory. */
export const SearchOnly: Story = {
  render: () => <FilterBar search={{ defaultValue: "", placeholder: "Search people" }} />,
};
