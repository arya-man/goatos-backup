import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import { expect, within } from "storybook/test";
import { Package } from "lucide-react";
import { StatStrip, type StatStripCell } from "@/components/minimal/widgets/stat-strip";
import { Canvas, MOBILE } from "../_data";

/** Template InvoiceAnalytic row split by dashed dividers; more than four cells wrap into rows (Feed stock has eight). */
const meta: Meta<typeof StatStrip> = {
  title: "Kit/StatStrip",
  component: StatStrip,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <Canvas>
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <Story />
        </Card>
      </Canvas>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof meta>;

const FEEDS = ["Mesha Kids Concentrate", "Mesha Adult Concentrate", "UHT Milk", "Dry Masoor Bhusa"];
const cells = (n: number): StatStripCell[] =>
  Array.from({ length: n }, (_, i) => ({
    key: `c${i}`,
    label: `${i % 2 ? "CBE" : "CPT"} · ${FEEDS[i % FEEDS.length]}`,
    value: `${4 + i * 3} days left`,
    meta: `${(1694.4 + i * 311).toLocaleString("en-IN")} kg in store · ${(379.6 - i * 20).toFixed(1)} kg/day`,
    icon: <Package size={18} />,
    tone: i === 0 ? "error" : "primary",
  }));

export const FourCells: Story = { args: { cells: cells(4), ariaLabel: "Feed stock" } };

export const EightCellsWrap: Story = {
  args: { cells: cells(8), ariaLabel: "Feed stock" },
  play: async ({ canvasElement }) => {
    const strip = within(canvasElement).getByRole("group", { name: "Feed stock" });
    await expect(strip.dataset.cols).toBe("4");
  },
};

export const SixCellsThreePerRow: Story = {
  args: { cells: cells(6), ariaLabel: "Feed stock" },
  play: async ({ canvasElement }) => {
    const strip = within(canvasElement).getByRole("group", { name: "Feed stock" });
    await expect(strip.dataset.cols).toBe("3");
  },
};

export const EightCellsMobile: Story = { args: { cells: cells(8), ariaLabel: "Feed stock" }, ...MOBILE };
