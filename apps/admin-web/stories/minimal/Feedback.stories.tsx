import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { EmptyContent } from "@/components/minimal/empty-content";
import { LoadingScreen } from "@/components/minimal/loading-screen/loading-screen";
import { SearchNotFound } from "@/components/minimal/search-not-found";
import { CustomBreadcrumbs } from "@/components/minimal/custom-breadcrumbs";
import { ProductItemSkeleton } from "@/components/minimal/skeletons/card-skeleton";
import { MailItemSkeleton, MailNavItemSkeleton } from "@/components/minimal/skeletons/list-skeleton";
import { withMinimalTheme, mobile } from "./_minimal";

function Breadcrumbs() {
  return (
    <CustomBreadcrumbs
      heading="Goats"
      links={[{ name: "Dashboard", href: "#" }, { name: "Herd", href: "#" }, { name: "Goats" }]}
      action={<Button variant="contained" startIcon={<Iconify icon="mingcute:add-line" />}>New goat</Button>}
      sx={{ mb: 3 }}
    />
  );
}

function All() {
  return (
    <Box sx={{ display: "grid", gap: 3 }}>
      <Breadcrumbs />
      <CustomBreadcrumbs heading="MSG-01040" backHref="#" links={[{ name: "Goats", href: "#" }, { name: "MSG-01040" }]} />
      <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", md: "repeat(3, minmax(0, 1fr))" } }}>
        <Card sx={{ height: 280 }}><EmptyContent filled title="No goats" description="Nothing matches this pen yet." /></Card>
        <Card sx={{ p: 3 }}><SearchNotFound query="MSG-9" /></Card>
        <Card sx={{ height: 280 }}><LoadingScreen /></Card>
      </Box>
      <Card sx={{ p: 3, display: "grid", gap: 1.5 }}>
        <Skeleton variant="text" sx={{ width: 0.4, typography: "h4" }} />
        <Skeleton variant="rounded" height={120} />
        <Box sx={{ display: "flex", gap: 2 }}>
          <Skeleton variant="circular" width={48} height={48} />
          <Box sx={{ flexGrow: 1 }}><Skeleton variant="text" /><Skeleton variant="text" sx={{ width: 0.6 }} /></Box>
        </Box>
      </Card>
      <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))", md: "repeat(4, minmax(0, 1fr))" } }}>
        <ProductItemSkeleton itemCount={4} />
      </Box>
      <Box sx={{ display: "grid", gap: 3, gridTemplateColumns: { xs: "1fr", md: "240px minmax(0, 1fr)" } }}>
        <Card sx={{ p: 2 }}><MailNavItemSkeleton itemCount={4} /></Card>
        <Card sx={{ p: 2 }}><MailItemSkeleton itemCount={4} /></Card>
      </Box>
      <Card sx={{ p: 0, height: 160 }}>
        <Scrollbar sx={{ p: 3 }}>
          {Array.from({ length: 20 }, (_, i) => (
            <Typography key={i} variant="body2">Scrollable line {i + 1}</Typography>
          ))}
        </Scrollbar>
      </Card>
      <Box sx={{ display: "flex", gap: 2, flexWrap: "wrap", color: "text.secondary" }}>
        {(["solar:home-angle-bold-duotone", "solar:users-group-rounded-bold-duotone", "solar:calendar-date-bold", "solar:medical-kit-bold", "solar:settings-bold-duotone", "solar:bell-bing-bold-duotone"] as const).map((i) => (
          <Iconify key={i} icon={i} width={28} />
        ))}
      </Box>
    </Box>
  );
}

const meta: Meta = { title: "Minimal/Feedback", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;
export const States: Story = { render: () => <All /> };
export const StatesMobile: Story = { render: () => <All />, globals: mobile };
