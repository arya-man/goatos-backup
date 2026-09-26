import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import CardHeader from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { AppWidgetSummary, AnalyticsWidgetSummary, BankingWidgetSummary, EcommerceWidgetSummary } from "@/components/minimal/widgets";
import { withMinimalTheme, mobile } from "./_minimal";

const WEEK = [21, 24, 22, 27, 29, 28, 33, 31];
const DAYS = ["19/09/2026", "20/09/2026", "21/09/2026", "22/09/2026", "23/09/2026", "24/09/2026", "25/09/2026", "26/09/2026"];
const spark = () => ({ categories: DAYS, series: WEEK });

const meta: Meta = { title: "Minimal/Widgets", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;

function AllWidgets() {
  return (
    <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", md: "repeat(3, minmax(0, 1fr))" } }}>
      <AppWidgetSummary title="Live goats" total={4812} percent={2.6} chart={spark()} />
      <AppWidgetSummary title="Deaths (7d)" total={12} percent={-0.4} chart={spark()} />
      <AppWidgetSummary title="Feed issued" total="1,240.5 kg" caption={null} />
      <AnalyticsWidgetSummary title="Weighings" total={71400} percent={2.6} icon={<Iconify icon="solar:chart-square-outline" width={48} />} chart={spark()} />
      <AnalyticsWidgetSummary title="Open tasks" total={132} percent={-0.1} color="warning" icon={<Iconify icon="solar:list-bold" width={48} />} chart={spark()} />
      <AnalyticsWidgetSummary title="Overdue" total={9} percent={4.2} color="error" icon={<Iconify icon="solar:danger-triangle-bold" width={48} />} />
      <EcommerceWidgetSummary title="Avg daily gain" total="92 g" percent={1.8} chart={spark()} />
      <EcommerceWidgetSummary title="Mortality" total="0.4%" percent={-0.3} caption="last 30 days" chart={spark()} />
      <BankingWidgetSummary title="Sales" total="₹4,99,900" percent={8.2} hint="Invoiced this month" chart={spark()} />
      <BankingWidgetSummary title="Expenses" total="₹1,20,400" percent={-6.6} color="warning" icon="eva:diagonal-arrow-right-up-fill" />
    </Box>
  );
}

export const Grid: Story = { render: () => <AllWidgets /> };
export const GridMobile: Story = { render: () => <AllWidgets />, globals: mobile };

function CardHeaders() {
  return (
    <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", md: "repeat(2, minmax(0, 1fr))" } }}>
      <Card>
        <CardHeader title="Weight trend" subheader="(+43%) than last month" action={<IconButton aria-label="More"><Iconify icon="eva:more-vertical-fill" /></IconButton>} />
        <CardContent><Typography variant="body2" sx={{ color: "text.secondary" }}>Card body</Typography></CardContent>
      </Card>
      <Card>
        <CardHeader title="Pens needing attention with a long title that wraps on phones" action={<Button size="small" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={18} />}>View all</Button>} />
        <CardContent><Typography variant="body2" sx={{ color: "text.secondary" }}>Card body</Typography></CardContent>
      </Card>
    </Box>
  );
}
export const CardHeaderAction: Story = { render: () => <CardHeaders /> };
export const CardHeaderActionMobile: Story = { render: () => <CardHeaders />, globals: mobile };
