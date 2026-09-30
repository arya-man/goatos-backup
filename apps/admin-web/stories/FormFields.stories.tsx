import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Box from "@mui/material/Box";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Radio from "@mui/material/Radio";
import RadioGroup from "@mui/material/RadioGroup";
import { Canvas, MOBILE, PENS, Stack } from "./_data";
import * as React from "react";

/**
 * Form fields on the template: MUI `TextField` (text, number, select, multiline), `Checkbox` /
 * `Radio` through `FormControlLabel`, and template buttons. The legacy `.fld` label + native
 * input / select / textarea classes are deleted.
 */
const meta = { title: "Kit/Form fields", parameters: { layout: "fullscreen" } } satisfies Meta;
export default meta;
type Story = StoryObj;

function Form() {
  const [pen, setPen] = React.useState("pen-b1");
  return (
    <Box component="form" sx={{ maxWidth: 520, display: "grid", gap: 2.5 }} onSubmit={(e: React.FormEvent) => e.preventDefault()}>
      <TextField id="f-tag" label="RFID tag" defaultValue="SF-048" placeholder="Scan the left ear tag" />
      <TextField id="f-weight" label="Weight (kg)" type="number" defaultValue="18.4" slotProps={{ htmlInput: { step: 0.1 } }} />
      <TextField
        select
        id="f-pen"
        label="Pen"
        value={pen}
        onChange={(event) => setPen(event.target.value)}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {PENS.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
      <TextField id="f-notes" label="Notes" multiline rows={3} defaultValue="Kid limping on the left hind leg; vet to review at the 14:00 round." />
      <Box sx={{ display: "flex", gap: 1.25 }}>
        <Button variant="contained" color="primary" type="submit">Save weighing</Button>
        <Button color="primary" variant="text">Cancel</Button>
      </Box>
    </Box>
  );
}

export const Default: Story = { render: () => (<Canvas><Form /></Canvas>) };

export const StatesMatrix: Story = {
  render: () => (
    <Canvas>
      <Stack title="empty / filled / disabled / read-only / error">
        <Box sx={{ maxWidth: 520, display: "grid", gap: 2.5 }}>
          <TextField id="s1" label="Vendor (empty)" placeholder="Search vendors" slotProps={{ inputLabel: { shrink: true } }} />
          <TextField id="s2" label="Vendor (filled)" defaultValue="Kranji Livestock Supply Cooperative" />
          <TextField id="s3" label="Vendor (disabled)" disabled defaultValue="Seletar Feed & Forage" />
          <TextField id="s4" label="Load ID (read-only)" defaultValue="LD-2481" slotProps={{ input: { readOnly: true } }} />
          <TextField id="s5" label="Weight (kg)" defaultValue="-4" error helperText="Weight must be greater than 0 kg." />
          <TextField id="s6" label="Long value overflow" defaultValue="Lim Chu Kang Goat Breeders Association — Northern Chapter, quarantine cleared 2026-04-18" />
        </Box>
      </Stack>
      <Stack title="checkbox / radio">
        <FormControlLabel control={<Checkbox defaultChecked />} label="Flag for vet review" />
        <RadioGroup defaultValue="doe" name="sex">
          <FormControlLabel value="doe" control={<Radio />} label="Doe" />
          <FormControlLabel value="buck" control={<Radio />} label="Buck" />
        </RadioGroup>
      </Stack>
    </Canvas>
  ),
};

export const Mobile: Story = { ...Default, ...MOBILE };
