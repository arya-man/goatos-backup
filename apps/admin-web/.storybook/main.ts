import { fileURLToPath } from "node:url";
import type { StorybookConfig } from "@storybook/nextjs-vite";

const appRoot = fileURLToPath(new URL("..", import.meta.url));

const config: StorybookConfig = {
  stories: [
    "../stories/**/*.mdx",
    "../stories/**/*.stories.@(ts|tsx)",
    // Config-owned health check story; keeps `storybook` verifiable before any
    // feature stories land.
    "./smoke/**/*.stories.@(ts|tsx)",
  ],
  addons: ["@storybook/addon-docs", "@storybook/addon-a11y"],
  framework: {
    name: "@storybook/nextjs-vite",
    options: {},
  },
  staticDirs: ["../public"],
  core: { disableTelemetry: true },
  typescript: { reactDocgen: "react-docgen-typescript" },
  async viteFinal(viteConfig) {
    // Tailwind 4 runs as a Vite plugin here (the app uses @tailwindcss/postcss
    // under Next). Same `app/globals.css` entry, same `@config` -> tailwind.config.ts.
    const { default: tailwindcss } = await import("@tailwindcss/vite");
    viteConfig.plugins = [...(viteConfig.plugins ?? []), tailwindcss()];
    viteConfig.resolve = {
      ...viteConfig.resolve,
      alias: {
        ...(viteConfig.resolve?.alias as Record<string, string> | undefined),
        "@": appRoot.replace(/\/$/, ""),
      },
    };
    return viteConfig;
  },
};

export default config;
