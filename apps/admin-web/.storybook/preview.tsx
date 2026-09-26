import * as React from "react";
import type { Decorator, Preview } from "@storybook/nextjs-vite";
import { configure } from "storybook/test";
import { THEME_STORAGE_KEY, type ThemeMode } from "../lib/theme";
import "./preview.css";
import { AppThemeStack } from "../theme/app-theme-provider";

/**
 * Applies a theme exactly the way the app does it (see lib/theme.ts
 * and the THEME_BOOT_SCRIPT in app/layout.tsx): `light`/`dark` class + a
 * `data-theme` attribute + `style.colorScheme` on <html>. The app's
 * `theme-switching` transition class is deliberately NOT used here so snapshots
 * are captured against settled colours.
 */
function applyStorybookTheme(mode: ThemeMode) {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  root.classList.toggle("light", mode === "light");
  root.classList.toggle("dark", mode === "dark");
  root.setAttribute("data-theme", mode);
  root.style.colorScheme = mode;
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    /* storage unavailable in some sandboxes: the choice lasts for this frame */
  }
}

/** Renders one story twice, light and dark, by re-hosting it in two iframes. */
function DualTheme({ storyId, height }: { storyId: string; height: number }) {
  const src = (mode: ThemeMode) =>
    `iframe.html?viewMode=story&id=${encodeURIComponent(storyId)}&globals=theme:${mode}`;
  return (
    <div style={{ display: "grid", gap: 12, gridTemplateColumns: "1fr 1fr", width: "100%" }}>
      {(["light", "dark"] as const).map((mode) => (
        <div key={mode} style={{ display: "grid", gap: 6 }}>
          <div style={{ font: "600 11px/1.4 var(--font-sans)", letterSpacing: ".06em", textTransform: "uppercase", opacity: 0.6 }}>
            {mode}
          </div>
          <iframe
            title={`${storyId}-${mode}`}
            src={src(mode)}
            style={{ width: "100%", height, border: "1px solid var(--line)", borderRadius: "var(--r-lg)" }}
          />
        </div>
      ))}
    </div>
  );
}

/** Applies the theme class from inside a component, so the layout effect is a legal hook call. */
function ThemedStory({ mode, children }: { mode: ThemeMode; children: React.ReactNode }) {
  // Layout effect so the class lands before the story paints.
  React.useLayoutEffect(() => {
    applyStorybookTheme(mode);
  }, [mode]);
  // Applied in the layout effect only: a render-time apply re-asserted the lane's theme on every
  // re-render and undid a ThemeToggle click mid-play (the light-lane "expected 'light' not to be
  // 'light'" failure).
  return <>{children}</>;
}

const withTheme: Decorator = (Story, context) => {
  const requested = (context.globals.theme ?? "dark") as ThemeMode | "both";
  const both = requested === "both";
  const mode: ThemeMode = both ? "dark" : requested;

  // The theme is applied by ThemedStory, which exists for exactly this: a decorator is a plain
  // function, so a hook called here is an illegal hook call (react-hooks/rules-of-hooks, a lint
  // ERROR and therefore a red `admin-web lint` CI step). It was declared with that comment and
  // then never wired, leaving the inline copy in place beside it.
  //
  // The render-time `applyStorybookTheme(mode)` that stood here is deliberately NOT carried over:
  // ThemedStory's own comment records why it was dropped -- it re-asserted the lane's theme on
  // every re-render and undid a ThemeToggle click mid-play (the light-lane "expected 'light' not
  // to be 'light'" failure). Each DualTheme pane is its own iframe running this preview with its
  // own mode, so the layout effect covers that path too.
  if (both) {
    const height = (context.parameters?.dualTheme?.height as number | undefined) ?? 720;
    return <DualTheme storyId={context.id} height={height} />;
  }
  return (
    <ThemedStory mode={mode}>
      <Story />
    </ThemedStory>
  );
};

export const globalTypes = {
  theme: {
    description: "App theme (html.light / html.dark, same as the top-bar toggle)",
    toolbar: {
      title: "Theme",
      icon: "paintbrush",
      items: [
        { value: "dark", title: "Dark", icon: "moon" },
        { value: "light", title: "Light", icon: "sun" },
        { value: "both", title: "Light + Dark", icon: "mirror" },
      ],
      dynamicTitle: true,
    },
  },
};

export const VIEWPORTS = {
  desktop: { name: "Desktop 1440 x 900", styles: { width: "1440px", height: "900px" }, type: "desktop" },
  laptop: { name: "Laptop 1280 x 800", styles: { width: "1280px", height: "800px" }, type: "desktop" },
  tablet: { name: "Tablet 768 x 1024", styles: { width: "768px", height: "1024px" }, type: "tablet" },
  mobile: { name: "Mobile 390 x 844", styles: { width: "390px", height: "844px" }, type: "mobile" },
} as const;

// Play functions wait on STATE (aria-expanded, role=menu present, indicator transform settled)
// through waitFor/findBy*. The library default gives those 1s, which is what turned the rowmenu /
// tablefooter / animatedtabs interaction stories red only under load (gate lane 4/3). The bound
// is generous on purpose: a wait that times out is a real broken state, not a slow laptop.
configure({ asyncUtilTimeout: 8_000 });

const preview: Preview = {
  tags: ["autodocs"],
  // Kit components are built on the MUI Minimal template (PageHeader, tabs, labels): every story
  // renders inside the app's theme stack (AppThemeStack = AppThemeProvider minus the App Router cache).
  decorators: [(Story) => <AppThemeStack><Story /></AppThemeStack>, withTheme],
  initialGlobals: {
    theme: "dark",
    viewport: { value: "desktop", isRotated: false },
  },
  parameters: {
    layout: "fullscreen",
    viewport: { options: VIEWPORTS },
    controls: { matchers: { color: /(background|color)$/i, date: /Date$/i } },
    a11y: { test: "todo" },
    options: { storySort: { order: ["Smoke", "Kit", "Features", "*"] } },
  },
};

export default preview;
