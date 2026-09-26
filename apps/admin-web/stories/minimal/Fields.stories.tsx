import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import Autocomplete from "@mui/material/Autocomplete";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";
import dayjs from "dayjs";
import { withMinimalTheme, mobile } from "./_minimal";

const PENS = ["P-01", "P-02", "P-03", "Kid shed", "Isolation"];

function Fields() {
  return (
    <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", md: "repeat(2, minmax(0, 1fr))" }, maxWidth: 880 }}>
      <TextField label="Tag" defaultValue="MSG-01040" />
      <TextField label="Weight (kg)" type="number" defaultValue={24.5} helperText="Latest weighing" />
      <TextField label="Pen" select defaultValue="P-01">
        {PENS.map((p) => <MenuItem key={p} value={p}>{p}</MenuItem>)}
      </TextField>
      <Autocomplete options={PENS} defaultValue="Kid shed" renderInput={(params) => <TextField {...params} label="Move to pen" />} />
      <TextField label="Notes" multiline minRows={3} placeholder="Observations" />
      <TextField label="Required" error helperText="Tag is required" />
      <TextField label="Disabled" disabled defaultValue="Read only" />
      <DatePicker label="Birth date" format="DD/MM/YYYY" defaultValue={dayjs("2026-03-14")} />
    </Box>
  );
}

const meta: Meta = { title: "Minimal/Fields", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;
export const Default: Story = { render: () => <Fields /> };
export const DefaultMobile: Story = { render: () => <Fields />, globals: mobile };
