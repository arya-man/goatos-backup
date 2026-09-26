import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { withMinimalTheme, mobile } from "./_minimal";

const COLORS = ["default", "primary", "secondary", "info", "success", "warning", "error"] as const;
const VARIANTS = ["filled", "outlined", "soft", "inverted"] as const;

function Matrix() {
  return (
    <Box sx={{ display: "grid", gap: 2 }}>
      {VARIANTS.map((v) => (
        <Box key={v} sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
          {COLORS.map((c) => (
            <Label key={c} variant={v} color={c}>{c}</Label>
          ))}
          <Label variant={v} color="success" startIcon={<Iconify icon="solar:check-circle-bold" />}>with icon</Label>
          <Label variant={v} disabled>disabled</Label>
        </Box>
      ))}
    </Box>
  );
}

const meta: Meta = { title: "Minimal/Label", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;
export const Variants: Story = { render: () => <Matrix /> };
export const VariantsMobile: Story = { render: () => <Matrix />, globals: mobile };
