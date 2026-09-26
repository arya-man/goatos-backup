import dayjs from "dayjs";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import { Iconify } from "@/components/minimal/iconify";
import { CustomDateRangePicker, useDateRangePicker } from "@/components/minimal/custom-date-range-picker";
import { withMinimalTheme, mobile } from "./_minimal";

function Demo({ open = false, variant = "calendar" }: { open?: boolean; variant?: "calendar" | "input" }) {
  const rp = useDateRangePicker(dayjs("2026-09-01"), dayjs("2026-09-14"));
  return (
    <Box>
      <Button variant="outlined" color="inherit" startIcon={<Iconify icon="solar:calendar-date-bold" />} onClick={rp.onOpen}>
        {rp.shortLabel}
      </Button>
      <CustomDateRangePicker
        variant={variant}
        open={open || rp.open}
        startDate={rp.startDate}
        endDate={rp.endDate}
        onChangeStartDate={rp.onChangeStartDate}
        onChangeEndDate={rp.onChangeEndDate}
        onClose={rp.onClose}
        error={rp.error}
      />
    </Box>
  );
}

const meta: Meta = { title: "Minimal/DateRangePicker", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;
export const Closed: Story = { render: () => <Demo /> };
export const CalendarOpen: Story = { render: () => <Demo open /> };
export const InputOpen: Story = { render: () => <Demo open variant="input" /> };
export const CalendarOpenMobile: Story = { render: () => <Demo open />, globals: mobile };
