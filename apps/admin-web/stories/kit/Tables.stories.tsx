import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { PagedRows } from "@/components/app/paged-rows";
import { DenseToggleAuto } from "@/components/app/dense-toggle-auto";
import { Frame, mobile } from "../_fixtures/frame";

/** Client-paged table rows (57px head / 72px rows) with the Dense toggle. */
const meta: Meta = {
  title: "Kit/PagedRows",
  parameters: { layout: "fullscreen", dualTheme: { height: 900 } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

const head = (<tr><th>Tag</th><th>Pen</th><th>Breed</th><th>Weight</th></tr>);
const rows = Array.from({ length: 37 }, (_, i) => (
  <tr key={i}><td>TAG-{1000 + i}</td><td>Pen {1 + (i % 6)}</td><td>{i % 2 ? "Boer" : "Sirohi"}</td><td>{(18 + (i % 9) * 1.5).toFixed(1)} kg</td></tr>
));

export const ManyRows: Story = {
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Animals" action={<DenseToggleAuto />} />
      <PagedRows ariaLabel="Animals" head={head} rows={rows} />
    </Card>
  ),
};

export const Empty: Story = {
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Animals" />
      <PagedRows ariaLabel="Animals" head={head} rows={[]} empty={<tr><td colSpan={4}>No animals in this pen</td></tr>} />
    </Card>
  ),
};

export const ManyRowsPhone: Story = { ...ManyRows, globals: mobile };

/** Dense on: rows stay compact on desktop (the 72px floor is released). */
export const Dense: Story = {
  render: () => (
    <Card data-dense="" sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Animals" />
      <PagedRows ariaLabel="Animals" head={head} rows={rows} />
    </Card>
  ),
};
