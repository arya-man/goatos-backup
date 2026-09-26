"use client";

// Template profile cover (sections/user/profile-cover.tsx + user-profile-view tabs) as a client
// component: function `sx` and the Link `component` cannot cross the server/client boundary.
// Cover image: template public/assets/images/mock/cover/cover-4.webp (the profile view's _userAbout.coverUrl).
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import MuiCard from "@mui/material/Card";
import Avatar from "@mui/material/Avatar";
import ListItemText from "@mui/material/ListItemText";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Link from "@/components/no-prefetch-link";
import { Iconify } from "@/components/minimal/iconify";
import { MINIMAL_ASSETS } from "@/components/minimal/_shared/config";
import { shownTabValue } from "@/components/app/url-tab-nav";
import { useUrlTabNav } from "@/components/app/use-url-tab-nav";

export type PassportCoverTab = { value: string; href: string; label: string };

export function PassportCover({
  displayId,
  secondaryLine,
  selectedTab,
  tabs,
}: {
  displayId: string;
  secondaryLine?: string;
  selectedTab: string;
  tabs: PassportCoverTab[];
}) {
  const { pendingValue, navigate } = useUrlTabNav();
  return (
    <MuiCard sx={{ mb: 3, height: { xs: 290 }, position: "relative" }}>
      {/* Template ProfileCover: the cover image under an 80% primary.darker veil, white type. */}
      <Box
        sx={(theme) => ({
          ...theme.mixins.bgGradient({
            // The Mesha `darker` step is a mid green (the template's is near-black teal), so a neutral
            // grey.900 veil goes over it to keep the template's dark cover and white-on-dark contrast.
            images: [
              `linear-gradient(0deg, ${varAlpha(theme.vars.palette.grey["900Channel"], 0.64)}, ${varAlpha(theme.vars.palette.grey["900Channel"], 0.64)})`,
              `linear-gradient(0deg, ${varAlpha(theme.vars.palette.primary.darkerChannel, 0.8)}, ${varAlpha(theme.vars.palette.primary.darkerChannel, 0.8)})`,
              `url(${MINIMAL_ASSETS}/background/cover-4.webp)`,
            ],
          }),
          height: 1,
          color: "common.white",
        })}
      >
        <Box
          sx={{
            display: "flex",
            left: { md: 24 },
            bottom: { md: 24 },
            zIndex: { md: 10 },
            pt: { xs: 6, md: 0 },
            position: { md: "absolute" },
            flexDirection: { xs: "column", md: "row" },
          }}
        >
          <Avatar
            alt={displayId}
            sx={(theme) => ({
              mx: "auto",
              width: { xs: 64, md: 128 },
              height: { xs: 64, md: 128 },
              border: `solid 2px ${theme.vars.palette.common.white}`,
              bgcolor: "primary.darker",
              color: "common.white",
            })}
          >
            <Iconify icon="solar:user-id-bold" sx={{ width: { xs: 32, md: 56 }, height: { xs: 32, md: 56 } }} />
          </Avatar>
          <ListItemText
            primary={displayId}
            secondary={secondaryLine || undefined}
            slotProps={{
              primary: { sx: { typography: "h4" } },
              secondary: { sx: { mt: 0.5, opacity: 0.48, color: "inherit" } },
            }}
            sx={{ mt: 3, ml: { md: 3 }, textAlign: { xs: "center", md: "unset" } }}
          />
        </Box>
      </Box>
      <Box
        sx={{
          width: 1,
          bottom: 0,
          zIndex: 9,
          px: { md: 3 },
          display: "flex",
          position: "absolute",
          bgcolor: "background.paper",
          justifyContent: { xs: "center", md: "flex-end" },
        }}
      >
        <Tabs value={shownTabValue(selectedTab, pendingValue)} aria-busy={pendingValue !== null || undefined} variant="scrollable" allowScrollButtonsMobile>
          {tabs.map((tab) => (
            <Tab key={tab.value || "summary"} component={Link} value={tab.value} href={tab.href} label={tab.label} onClick={(event: React.MouseEvent<HTMLElement>) => navigate(event, tab.value, tab.href)} />
          ))}
        </Tabs>
      </Box>
    </MuiCard>
  );
}
