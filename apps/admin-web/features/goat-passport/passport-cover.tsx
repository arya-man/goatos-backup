"use client";

// Template profile cover (sections/user/profile-cover.tsx + user-profile-view tabs) as a client
// component: function `sx` and the Link `component` cannot cross the server/client boundary.
import Box from "@mui/material/Box";
import MuiCard from "@mui/material/Card";
import Avatar from "@mui/material/Avatar";
import ListItemText from "@mui/material/ListItemText";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Link from "@/components/no-prefetch-link";

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
  return (
    <MuiCard sx={{ height: { xs: 290 }, position: "relative" }}>
      <Box
        sx={(theme) => ({
          position: "absolute",
          inset: 0,
          bgcolor: theme.vars.palette.primary.darker,
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
              borderWidth: 2,
              borderStyle: "solid",
              borderColor: theme.vars.palette.common.white,
              bgcolor: theme.vars.palette.primary.dark,
              typography: "h3",
            })}
          >
            {displayId.slice(0, 2).toUpperCase()}
          </Avatar>
          <ListItemText
            primary={displayId}
            secondary={secondaryLine || undefined}
            slotProps={{
              primary: { sx: { typography: "h4" } },
              secondary: { sx: { mt: 0.5, opacity: 0.72, color: "inherit" } },
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
        <Tabs value={selectedTab} variant="scrollable" allowScrollButtonsMobile>
          {tabs.map((tab) => (
            <Tab key={tab.value || "summary"} component={Link} value={tab.value} href={tab.href} label={tab.label} />
          ))}
        </Tabs>
      </Box>
    </MuiCard>
  );
}
