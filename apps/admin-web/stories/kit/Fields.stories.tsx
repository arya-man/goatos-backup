import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { DateTimeField } from "@/components/app/date-time-field";
import { TimeField } from "@/components/app/time-field";
import { Frame, Grid, Labelled, mobile } from "../_fixtures/frame";

/** Date+time and time pickers (the kit replacement for native date/time inputs). */
const meta: Meta = {
  title: "Kit/Fields",
  parameters: { layout: "fullscreen", dualTheme: { height: 520 } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

const dtCopy = { hourLabel: "Hour", minuteLabel: "Minute", previousMonthLabel: "Previous month", nextMonthLabel: "Next month", invalidDateText: "Pick a valid date" };

export const States: Story = {
  render: () => (
    <Grid min={300}>
      <Labelled label="Date + time: empty"><DateTimeField name="a" label="Administered at" {...dtCopy} /></Labelled>
      <Labelled label="Date + time: filled"><DateTimeField name="b" label="Administered at" defaultValue="2026-09-14T09:30" {...dtCopy} /></Labelled>
      <Labelled label="Date + time: required, bounded"><DateTimeField name="c" label="Due" required min="2026-09-01" max="2026-09-30" {...dtCopy} /></Labelled>
      <Labelled label="Time: empty"><TimeField name="d" hourLabel="Hour" minuteLabel="Minute" ariaLabel="Start time" /></Labelled>
      <Labelled label="Time: filled"><TimeField name="e" hourLabel="Hour" minuteLabel="Minute" defaultValue="14:05" ariaLabel="Start time" /></Labelled>
      <Labelled label="Time: required"><TimeField name="f" hourLabel="Hour" minuteLabel="Minute" required ariaLabel="End time" /></Labelled>
    </Grid>
  ),
};

export const Phone: Story = { ...States, globals: mobile };
