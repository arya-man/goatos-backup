import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Switch from "@mui/material/Switch";
import Slider from "@mui/material/Slider";
import FormControlLabel from "@mui/material/FormControlLabel";
import Tooltip from "@mui/material/Tooltip";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import Radio from "@mui/material/Radio";
import { Avatar, AvatarGroup } from "@/components/app/avatar";
import { Label } from "@/components/minimal/label";
import { ProgressBar } from "@/components/app/progress-bar";
import { Frame, Grid, Labelled, mobile } from "../_fixtures/frame";

/** Minimal Checkbox / Radio / Slider / Alert / Avatar: default, checked, hover, focus, disabled, error. */
const meta: Meta = {
  title: "Kit/Controls",
  parameters: { layout: "fullscreen", dualTheme: { height: 760 } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

function SwitchDemo() {
  const [a, setA] = React.useState(false);
  const [b, setB] = React.useState(true);
  return (
    <div style={{ display: "grid" }}>
      <FormControlLabel control={<Switch checked={a} onChange={(e) => setA(e.target.checked)} />} label="Domain column" />
      <FormControlLabel control={<Switch checked={b} onChange={(e) => setB(e.target.checked)} />} label="Trigger column" />
      <FormControlLabel control={<Switch checked disabled onChange={() => {}} />} label="Locked" />
    </div>
  );
}

function SliderDemo({ initial, size, disabled }: { initial: number; size?: "small" | "medium"; disabled?: boolean }) {
  const [v, setV] = React.useState(initial);
  return (
    <Slider
      aria-label="Target weight"
      value={v}
      onChange={(_e, next) => setV(Array.isArray(next) ? next[0] : next)}
      size={size}
      disabled={disabled}
    />
  );
}

export const States: Story = {
  render: () => (
    <Grid min={300}>
      <Labelled label="Checkbox">
        <div style={{ display: "grid" }}>
          <FormControlLabel control={<Checkbox sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Unchecked" />
          <FormControlLabel control={<Checkbox defaultChecked sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Checked" />
          <FormControlLabel control={<Checkbox defaultChecked sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Checked (primary)" />
          <FormControlLabel disabled control={<Checkbox disabled sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Disabled" />
          <FormControlLabel disabled control={<Checkbox disabled defaultChecked sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Disabled checked" />
        </div>
      </Labelled>
      <Labelled label="Radio">
        <div style={{ display: "grid" }} role="radiogroup" aria-label="Pen">
          <FormControlLabel control={<Radio name="pen" defaultChecked sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Pen A-01" />
          <FormControlLabel control={<Radio name="pen" sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Pen B-02" />
          <FormControlLabel control={<Radio name="pen2" defaultChecked sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Primary" />
          <FormControlLabel disabled control={<Radio name="pen3" disabled sx={{ p: { xs: 1.5, sm: 1 } }} />} label="Disabled" />
        </div>
      </Labelled>
      <Labelled label="Slider (small / medium / disabled / empty)">
        <div style={{ display: "grid", gap: 16 }}>
          <SliderDemo initial={40} />
          <SliderDemo initial={70} size="medium" />
          <SliderDemo initial={30} disabled />
          <SliderDemo initial={0} />
        </div>
      </Labelled>
      <Labelled label="Alert: standard">
        <div style={{ display: "grid", gap: 12 }}>
          <Alert severity="success">Load 2411 received — 48 head.</Alert>
          <Alert severity="info"><AlertTitle>Heads up</AlertTitle>Weighing round starts at 07:00.</Alert>
          <Alert severity="warning">3 animals overdue for PPR booster.</Alert>
          <Alert severity="error" action={<Button color="primary" size="small" variant="text">Retry</Button>}>Could not save the pen transfer.</Alert>
        </div>
      </Labelled>
      <Labelled label="Alert: outlined">
        <div style={{ display: "grid", gap: 12 }}>
          <Alert variant="outlined" severity="success">Saved.</Alert>
          <Alert variant="outlined" severity="error"><AlertTitle>Upload failed</AlertTitle>The sheet has no Tag column.</Alert>
        </div>
      </Labelled>
      <Labelled label="Label (soft / filled / zero / big)">
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <span>Filters <Label variant="soft">3</Label></span>
          <Label variant="filled">12</Label>
          <Label variant="soft">0</Label>
          <Label variant="soft">1,248</Label>
        </div>
      </Labelled>
      <Labelled label="Switch (off / on / disabled)">
        <SwitchDemo />
      </Labelled>
      <Labelled label="ProgressBar (0 / 40 / 100 / error colour)">
        <div style={{ display: "grid", gap: 12, width: 200 }}>
          <ProgressBar value={0} />
          <ProgressBar value={40} />
          <ProgressBar value={100} />
          <ProgressBar value={65} color="var(--error)" />
        </div>
      </Labelled>
      <Labelled label="Avatar">
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <Avatar name="Ravi Teja" />
          <Avatar name="Manohar" variant="rounded" />
          <Avatar name="Satish Kumar" size={32} />
          <Avatar name="" />
        </div>
        <div style={{ display: "flex", gap: 16, alignItems: "center", marginTop: 12 }}>
          <AvatarGroup names={["Ravi Teja", "Manohar", "Satish Kumar"]} />
          <AvatarGroup names={["Ravi Teja", "Manohar", "Satish Kumar", "Manju", "Anil"]} />
          <AvatarGroup names={["Ravi Teja"]} extra={6} size={22} />
          <AvatarGroup names={[]} empty="!" emptyTone="danger" size={22} />
        </div>
      </Labelled>
    </Grid>
  ),
};

export const Phone: Story = { ...States, globals: mobile };

/** Tooltip: portaled bubble, opens on hover/focus/tap. The story focuses the first trigger so the capture shows it open. */
export const TooltipOpen: Story = {
  render: () => {
    const Auto = () => {
      const ref = React.useRef<HTMLDivElement>(null);
      React.useEffect(() => { ref.current?.querySelector<HTMLElement>("button")?.focus(); }, []);
      return (
        <div ref={ref} style={{ display: "flex", gap: 48, padding: "72px 24px", flexWrap: "wrap" }}>
          <Tooltip title="Share of due animals vaccinated inside the safe window" placement="top"><Button color="primary" size="small" variant="outlined">Coverage</Button></Tooltip>
          <Tooltip title="Bottom placement" placement="bottom"><Button color="primary" size="small" variant="text">Below</Button></Tooltip>
          <Tooltip title="A longer explanation wraps at 300px and is clamped to the viewport edge on a phone so it never runs off screen." placement="top" slotProps={{ tooltip: { sx: { maxWidth: 300 } } }}><Button color="primary" size="small" variant="soft">Wide</Button></Tooltip>
        </div>
      );
    };
    return <Auto />;
  },
};

export const TooltipPhone: Story = { ...TooltipOpen, globals: mobile };
