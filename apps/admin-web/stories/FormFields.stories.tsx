import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { Canvas, MOBILE, PENS, Stack } from "./_data";
import * as React from "react";

/**
 * Native form fields styled by the theme (`.fld` label + input/select/textarea), shown next to the
 * MUI `TextField select` so a form mixing both stays on one baseline.
 */
const meta = { title: "Kit/Form fields", parameters: { layout: "fullscreen" } } satisfies Meta;
export default meta;
type Story = StoryObj;

function Form() {
  const [pen, setPen] = React.useState("pen-b1");
  return (
    <form style={{ maxWidth: 520, display: "grid", gap: 4 }} onSubmit={(e) => e.preventDefault()}>
      <div className="fld">
        <label htmlFor="f-tag">RFID tag</label>
        <input id="f-tag" defaultValue="SF-048" placeholder="Scan the left ear tag" />
      </div>
      <div className="fld">
        <label htmlFor="f-weight">Weight (kg)</label>
        <input id="f-weight" type="number" step="0.1" defaultValue="18.4" />
      </div>
      <div className="fld">
        <label htmlFor="f-pen">Pen (MUI TextField select)</label>
        <TextField
          select
          label="Pen"
          value={pen}
          onChange={(event) => setPen(event.target.value)}
          sx={{ minWidth: { xs: 0, sm: 240 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {PENS.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
      </div>
      <div className="fld">
        <label htmlFor="f-notes">Notes</label>
        <textarea id="f-notes" rows={3} defaultValue="Kid limping on the left hind leg; vet to review at the 14:00 round." />
      </div>
      <div style={{ display: "flex", gap: 10, marginTop: 8 }}>
        <Button variant="contained" color="primary" type="submit">Save weighing</Button>
        <Button color="primary" variant="text">Cancel</Button>
      </div>
    </form>
  );
}

export const Default: Story = { render: () => (<Canvas><Form /></Canvas>) };

export const StatesMatrix: Story = {
  render: () => (
    <Canvas>
      <Stack title="empty / filled / disabled / read-only / error">
        <div style={{ maxWidth: 520 }}>
          <div className="fld"><label htmlFor="s1">Vendor (empty)</label><input id="s1" placeholder="Search vendors" /></div>
          <div className="fld"><label htmlFor="s2">Vendor (filled)</label><input id="s2" defaultValue="Kranji Livestock Supply Cooperative" /></div>
          <div className="fld"><label htmlFor="s3">Vendor (disabled)</label><input id="s3" disabled defaultValue="Seletar Feed &amp; Forage" /></div>
          <div className="fld"><label htmlFor="s4">Load ID (read-only)</label><input id="s4" readOnly defaultValue="LD-2481" /></div>
          <div className="fld">
            <label htmlFor="s5">Weight (kg)</label>
            <input id="s5" defaultValue="-4" aria-invalid="true" style={{ borderColor: "var(--error)", boxShadow: "0 0 0 3px var(--error-soft)" }} />
            <span style={{ color: "var(--error-ink)", fontSize: 12 }}>Weight must be greater than 0 kg.</span>
          </div>
          <div className="fld">
            <label htmlFor="s6">Long value overflow</label>
            <input id="s6" defaultValue="Lim Chu Kang Goat Breeders Association — Northern Chapter, quarantine cleared 2026-04-18" />
          </div>
        </div>
      </Stack>
      <Stack title="checkbox / radio">
        <label style={{ display: "flex", gap: 8, alignItems: "center" }}><input type="checkbox" defaultChecked /> Flag for vet review</label>
        <label style={{ display: "flex", gap: 8, alignItems: "center" }}><input type="radio" name="sex" defaultChecked /> Doe</label>
        <label style={{ display: "flex", gap: 8, alignItems: "center" }}><input type="radio" name="sex" /> Buck</label>
      </Stack>
    </Canvas>
  ),
};

export const Mobile: Story = { ...Default, ...MOBILE };
