import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import Chip from "@mui/material/Chip";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";

/**
 * Config-owned health check: proves the MUI theme (AppThemeStack: palette, typography, fonts) and
 * the light/dark switch are wired into Storybook. It renders MUI parts only and imports nothing from
 * components/ or features/, so it stays green while those are being edited. No app stylesheet
 * carries the look any more (the legacy theme CSS is deleted); the palette comes from the theme.
 */
const SWATCHES = ["background.default", "background.paper", "background.neutral", "primary.main", "primary.dark", "info.main", "warning.main", "error.main"] as const;
const STATE_COLOR = { Overdue: "error", Done: "success", Due: "warning" } as const;

function ThemeSmoke() {
  const rows = [
    { id: "SF-048", shed: "Godel 1 - Part 1", due: 12, state: "Due" as const },
    { id: "SF-112", shed: "Godel 2 - Part 3", due: 4, state: "Done" as const },
    { id: "SF-203", shed: "Godel 3 - Part 1", due: 27, state: "Overdue" as const },
  ];
  return (
    <Box sx={{ p: 3, bgcolor: "background.default", color: "text.primary", minHeight: "100vh" }}>
      <Typography variant="h5" component="h1" sx={{ mb: 0.5 }}>Storybook theme smoke</Typography>
      <Typography variant="body2" sx={{ color: "text.secondary", mb: 2.5 }}>
        Public Sans, brand palette and surfaces, straight from the MUI theme.
      </Typography>

      <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", mb: 2.5 }}>
        {SWATCHES.map((token) => (
          <Paper key={token} variant="outlined" sx={{ width: 116, overflow: "hidden" }}>
            <Box sx={{ height: 40, bgcolor: token }} />
            <Typography variant="caption" component="div" sx={{ px: 1, py: 0.75, color: "text.secondary" }}>{token}</Typography>
          </Paper>
        ))}
      </Box>

      <Card sx={{ p: 2, maxWidth: 680 }}>
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 1.5 }}>
          <Typography variant="subtitle1" component="strong">Vaccination queue</Typography>
          <Button variant="contained" color="primary">Start round</Button>
        </Box>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Tag</TableCell>
              <TableCell>Partition</TableCell>
              <TableCell>Due</TableCell>
              <TableCell>State</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell>{row.id}</TableCell>
                <TableCell>{row.shed}</TableCell>
                <TableCell>{row.due}</TableCell>
                <TableCell>
                  <Chip size="small" variant="soft" color={STATE_COLOR[row.state]} label={row.state} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </Box>
  );
}

const meta = {
  title: "Smoke/Theme",
  component: ThemeSmoke,
  parameters: { layout: "fullscreen", dualTheme: { height: 760 } },
} satisfies Meta<typeof ThemeSmoke>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Mobile: Story = { globals: { viewport: { value: "mobile", isRotated: false } } };
