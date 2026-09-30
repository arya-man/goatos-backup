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
    // No Tailwind (FIXJ7, guard `tailwind-banned`): stories style through the MUI theme stack.
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
