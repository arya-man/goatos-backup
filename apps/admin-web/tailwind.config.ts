import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: "class",
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        sidebar: {
          DEFAULT: "hsl(var(--sidebar-background))",
          foreground: "hsl(var(--sidebar-foreground))",
          primary: "hsl(var(--sidebar-primary))",
          "primary-foreground": "hsl(var(--sidebar-primary-foreground))",
          accent: "hsl(var(--sidebar-accent))",
          "accent-foreground": "hsl(var(--sidebar-accent-foreground))",
          border: "hsl(var(--sidebar-border))",
          ring: "hsl(var(--sidebar-ring))",
        },
        /* Mesha design system. These were raw hexes, so `bg-surface`,
           `text-positive` etc. never tracked the theme and stayed dark-mode
           coloured on light paper. They now read the same CSS variables
           mesha-theme.css defines, so they follow both themes. */
        surface: {
          DEFAULT: "var(--panel)",
          muted: "var(--panel-2)",
        },
        zinc: {
          750: "var(--line)",
          850: "var(--line2)",
        },
        positive: "var(--ok)",
        negative: "var(--danger)",
        brand: "var(--brand)",
        value: "var(--value)",
      },
      borderRadius: {
        /* The system is 2px on surfaces and a full pill on controls; the old
           lg/md/sm ramp subtracted px off a 10px radius and produced negatives
           against the 2px token. All three collapse to the system radius. */
        lg: "var(--r)",
        md: "var(--r)",
        sm: "var(--r)",
        pill: "var(--r-pill)",
      },
      fontFamily: {
        sans: ["var(--f)", "Instrument Sans", "system-ui", "sans-serif"],
        serif: ["var(--f-serif)", "Instrument Serif", "Georgia", "serif"],
        mono: ["var(--fm)", "Spline Sans Mono", "ui-monospace", "monospace"],
      },
    },
  },
  plugins: [],
};
export default config;
