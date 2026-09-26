import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { Caption } from "@/components/app/caption";
import { InfoHint } from "@/components/app/info-hint";
import { EmptyState } from "@/components/app/empty-state";
import { Frame, Grid, Labelled, mobile } from "../_fixtures/frame";

/** Empty states, info hints and captions: the only sanctioned "explain this" surfaces. */
const meta: Meta = {
  title: "Kit/Feedback",
  parameters: { layout: "fullscreen", dualTheme: { height: 560 } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

export const States: Story = {
  render: () => (
    <Grid min={300}>
      <Labelled label="Empty state"><Card sx={{ p: { xs: 2, sm: 3 } }}><EmptyState title="No drives in this window" /></Card></Labelled>
      <Labelled label="Empty state: long title"><Card sx={{ p: { xs: 2, sm: 3 } }}><EmptyState title="No animals match these filters across every pen in all parks for the selected dates" /></Card></Labelled>
      <Labelled label="Info hint (focusable)"><span>Coverage <InfoHint text="Share of due animals vaccinated inside the safe window." /></span></Labelled>
      <Labelled label="Caption: short"><Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Weights" /><Caption>Last 30 days</Caption></Card></Labelled>
      <Labelled label="Caption: long collapses to hint"><Card sx={{ p: { xs: 2, sm: 3 } }}><CardHeader sx={{ p: 0, mb: 2 }} title="Weights" /><Caption>{"Average daily gain is measured between the two most recent weighings of the same animal, excluding sessions flagged for re-weigh."}</Caption></Card></Labelled>
    </Grid>
  ),
};

export const Phone: Story = { ...States, globals: mobile };
