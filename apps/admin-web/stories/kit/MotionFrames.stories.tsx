import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import Drawer from "@mui/material/Drawer";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Button from "@mui/material/Button";
import { TrendChart } from "@/components/app/trend-chart";
import { RowMenu } from "@/components/app/row-menu";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { Canvas, PEN_ROWS } from "../_data";

/**
 * Interaction-frame stories. `parameters.motionFrames` tells smoke-stories-visual.mjs to
 * re-open the story with animations LIVE, perform `trigger` under paused virtual time, then
 * advance the clock to each `at` millisecond and capture a frame. Every frame is diffed against
 * its own baseline, so a flicker, a height jump, a missing tab indicator or a chart that does not
 * draw in shows up as a red frame with a diff image — not as "the settled screenshot looked fine".
 *
 * `trigger.selector` is a CSS selector or `role=<role>[name=<text>]`; `trigger.type` is click|hover.
 */
const meta: Meta = {
  title: "Kit/MotionFrames",
  parameters: { layout: "fullscreen", a11y: { test: "off" } },
  decorators: [
    (Story) => (
      <Canvas>
        <Story />
      </Canvas>
    ),
  ],
};
export default meta;
type Story = StoryObj;

const tabItems = [
  { value: "summary", label: "Summary" },
  { value: "breed", label: "Breed mix", count: 5 },
  { value: "sheds", label: "Pens", count: 12 },
];

function TabsDemo() {
  const [value, setValue] = React.useState("summary");
  return (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <TemplateTabs items={tabItems} value={value} onChange={setValue} ariaLabel="Herd sections" />
      <TabPanel tabKey={value}>
        <div style={{ padding: "16px 0", minHeight: 120 }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title={tabItems.find((t) => t.value === value)?.label ?? ""} />
          <p style={{ margin: 0, fontSize: 14, color: "var(--fg-muted)" }}>
            {value === "summary" ? "2,302 head · 12 pens" : value === "breed" ? "Boer 812 · Jamnapari 604 · Sirohi 421" : "Pen 01 – Pen 12"}
          </p>
        </div>
      </TabPanel>
    </Card>
  );
}

/** Click "Breed mix": indicator slides, panel crossfades without a height jump. */
export const TabsClick: Story = {
  render: () => <TabsDemo />,
  parameters: { motionFrames: { trigger: { type: "click", selector: "role=tab[name=Breed mix]" }, at: [0, 60, 180, 300] } },
};

function DialogDemo() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button color="primary" variant="contained" onClick={() => setOpen(true)}>
        Open dialog
      </Button>
      <Dialog fullWidth maxWidth="xs" open={open} onClose={() => setOpen(false)} slotProps={{ paper: { "aria-label": "Confirm cull list" } }}>
        <DialogTitle sx={{ pb: 2 }}>Confirm cull list</DialogTitle>
        <DialogContent sx={{ typography: "body2" }}>12 animals in Pen B2 will be marked for sale.</DialogContent>
        <DialogActions>
          <Button color="primary" variant="outlined" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button color="primary" variant="contained">Confirm</Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

/** MUI Dialog fade-in with backdrop fade. */
export const DialogOpen: Story = {
  render: () => <DialogDemo />,
  parameters: { motionFrames: { trigger: { type: "click", selector: "role=button[name=Open dialog]" }, at: [0, 60, 180, 300] } },
};

function SheetDemo() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <Button color="primary" variant="contained" onClick={() => setOpen(true)}>
        Open drawer
      </Button>
      <Drawer anchor="right" open={open} onClose={() => setOpen(false)} slotProps={{ paper: { "aria-label": "Pen SF-0048", sx: { width: 1, maxWidth: 480 } } }}>
        <div style={{ display: "grid", gap: 12, padding: 24 }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Pen SF-0048</h2>
          <dl style={{ margin: 0, display: "grid", gridTemplateColumns: "auto 1fr", gap: "8px 16px", fontSize: 14 }}>
            <dt style={{ color: "var(--fg-muted)" }}>Head</dt>
            <dd style={{ margin: 0 }}>46</dd>
            <dt style={{ color: "var(--fg-muted)" }}>ADG</dt>
            <dd style={{ margin: 0 }}>168 g/day</dd>
            <dt style={{ color: "var(--fg-muted)" }}>Vendor</dt>
            <dd style={{ margin: 0 }}>Kranji Livestock</dd>
          </dl>
        </div>
      </Drawer>
    </>
  );
}

/** Drawer slides in from the right with backdrop fade. */
export const DrawerOpen: Story = {
  render: () => <SheetDemo />,
  parameters: { motionFrames: { trigger: { type: "click", selector: "role=button[name=Open drawer]" }, at: [0, 60, 180, 300] } },
};

const rowActions = [
  { label: "Open pen", onSelect: () => {} },
  { label: "Move animals", onSelect: () => {} },
  { label: "Archive", onSelect: () => {}, danger: true },
];

/** Row menu on the LAST row: the popover must flip up and never clip at the card edge. */
export const RowMenuLastRow: Story = {
  render: () => (
    <Card>
      <Table>
        <TableHead>
          <TableRow>
            <TableCell>Pen</TableCell>
            <TableCell>Kids</TableCell>
            <TableCell>ADG</TableCell>
            <TableCell sx={{ width: 56 }} />
          </TableRow>
        </TableHead>
        <TableBody>
          {PEN_ROWS.map((r, i) => (
            <TableRow key={r.pen}>
              <TableCell>{r.pen}</TableCell>
              <TableCell>{r.kids}</TableCell>
              <TableCell>{r.adg} g/day</TableCell>
              <TableCell>
                <RowMenu actions={rowActions} ariaLabel={`${r.pen} actions`} className={i === PEN_ROWS.length - 1 ? "last-row-menu" : undefined} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  ),
  parameters: { motionFrames: { trigger: { type: "click", selector: "role=button[name=Pen C1 actions]" }, at: [0, 60, 180] } },
};

const intake = [
  { month: "Apr", intake: 180, sold: 120 },
  { month: "May", intake: 220, sold: 160 },
  { month: "Jun", intake: 260, sold: 210 },
  { month: "Jul", intake: 240, sold: 230 },
  { month: "Aug", intake: 300, sold: 250 },
  { month: "Sep", intake: 280, sold: 270 },
];

/** Hover the chart: the tooltip card appears near the pointer, the hovered mark lightens. */
export const ChartHover: Story = {
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Kid intake vs sold" />
      <div data-motion-target style={{ height: 280 }}>
        <TrendChart data={intake} xKey="month" kind="bar" series={[{ key: "intake", label: "Intake" }, { key: "sold", label: "Sold" }]} height={260} />
      </div>
    </Card>
  ),
  parameters: { motionFrames: { trigger: { type: "hover", selector: "[data-motion-target]", position: { x: 0.62, y: 0.5 } }, at: [60, 800] } },
};

function DrawInDemo() {
  const [show, setShow] = React.useState(false);
  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div>
        <Button color="primary" variant="contained" onClick={() => setShow(true)}>
          Show chart
        </Button>
      </div>
      <div style={{ display: "grid", gap: 16, gridTemplateColumns: "repeat(auto-fit, minmax(min(320px, 100%), 1fr))" }}>
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="Kid intake" />
          <div style={{ height: 260 }}>
            {show ? (
              <TrendChart data={intake} xKey="month" kind="area" series={[{ key: "intake", label: "Intake" }]} height={240} />
            ) : (
              <div style={{ display: "grid", height: "100%", placeItems: "center", color: "var(--fg-muted)", fontSize: 14 }}>
                Chart ready
              </div>
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}

/** Mount the chart: area draws left→right; frames at 150/450/900ms. */
export const ChartDrawIn: Story = {
  render: () => <DrawInDemo />,
  parameters: { motionFrames: { trigger: { type: "click", selector: "role=button[name=Show chart]" }, at: [150, 450, 900, 1800] } },
};
