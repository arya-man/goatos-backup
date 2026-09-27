import * as React from "react";
import type { Decorator } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import { useColorScheme } from "@mui/material/styles";
import { AdapterDayjs } from "@mui/x-date-pickers/AdapterDayjs";
import { LocalizationProvider } from "@mui/x-date-pickers/LocalizationProvider";
import { ThemeProvider } from "@/theme";
import { defaultSettings, SettingsProvider } from "@/layouts/template/settings";
import { PhoneTapStyles } from "@/components/app/phone-tap-styles";

/**
 * Renders a story inside the Minimal (MUI) theme that Phase 1 installs under apps/admin-web/theme
 * (same provider stack as theme/app-theme-provider.tsx), following the Storybook `theme` global.
 * The preview already writes html[data-theme]; MUI's colorSchemeSelector is that attribute.
 * It cannot wrap AppThemeProvider itself: AppRouterCacheProvider logs "not compatible with the
 * Pages Router" under Storybook. Keep this list in step with AppThemeProvider until it exports its
 * inner stack without the cache provider.
 */
function SyncMode({ mode }: { mode: "light" | "dark" }) {
  const { setMode } = useColorScheme();
  React.useLayoutEffect(() => {
    setMode(mode);
  }, [mode, setMode]);
  return null;
}

export const withMinimalTheme: Decorator = (Story, context) => {
  const requested = context.globals.theme as string | undefined;
  const mode: "light" | "dark" = requested === "light" ? "light" : "dark";
  return (
    <SettingsProvider defaultSettings={defaultSettings}>
      <ThemeProvider defaultMode={mode}>
        <PhoneTapStyles />
        <SyncMode mode={mode} />
        <LocalizationProvider dateAdapter={AdapterDayjs}>
          <Box sx={{ p: { xs: 2, md: 5 }, minHeight: "100dvh", bgcolor: "background.default", color: "text.primary", maxWidth: "100vw", overflowX: "hidden" }}>
            <Story />
          </Box>
        </LocalizationProvider>
      </ThemeProvider>
    </SettingsProvider>
  );
};

export const mobile = { viewport: { value: "mobile", isRotated: false } } as const;
