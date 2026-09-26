import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { Frame, mobile } from "../_fixtures/frame";

/**
 * The calendar inside the date pickers, drawn OPEN so the lane captures it: Minimal DateCalendar
 * circles (36px, 44px on phones), a soft brand band for a span with filled ends, today's ring,
 * faded outside-month days, and the small data dots under the number.
 */
const meta: Meta = {
  title: "Kit/DateCalendar",
  parameters: { layout: "fullscreen", dualTheme: { height: 560 } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

const labels: DateRangePickerLabels = {
  field: "Period",
  today: "Today",
  single: "Single day",
  range: "Date range",
  aria: "Choose the reporting period",
  previousMonth: "Previous month",
  nextMonth: "Next month",
  rangeStartHint: "Pick the first day of the range.",
  rangeEndHint: "Pick the last day of the range.",
  rangeSeparator: "to",
  markerHint: "Weighings recorded on this day",
};

const markers = ["2026-09-02", "2026-09-09", "2026-09-16", "2026-09-19", "2026-09-23"];

/** Opens every picker in the story once it has mounted (a closed <details> hides the calendar). */
function Opened({ children }: { children: React.ReactNode }) {
  const ref = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    ref.current?.querySelectorAll("details").forEach((details) => {
      details.open = true;
    });
  }, []);
  return (
    <div ref={ref} style={{ display: "flex", flexWrap: "wrap", gap: "var(--sp-3)", alignItems: "flex-start", minHeight: 520 }}>
      {children}
    </div>
  );
}

function Picker({ from, to, singleDayOnly }: { from: string; to: string; singleDayOnly?: boolean }) {
  const [span, setSpan] = React.useState({ from, to });
  return (
    <div style={{ width: 320 }}>
      <DateRangePicker
        labels={labels}
        from={span.from}
        to={span.to}
        today="2026-09-26"
        markerDates={markers}
        minDate="2026-08-01"
        singleDayOnly={singleDayOnly}
        onChange={(nextFrom, nextTo) => setSpan({ from: nextFrom, to: nextTo })}
      />
    </div>
  );
}

export const Range: Story = {
  render: () => (
    <Opened>
      <Picker from="2026-09-08" to="2026-09-23" />
    </Opened>
  ),
};

export const SingleDay: Story = {
  render: () => (
    <Opened>
      <Picker from="2026-09-16" to="2026-09-16" />
    </Opened>
  ),
};

export const FormDate: Story = {
  render: () => (
    <Opened>
      <div style={{ width: 320 }}>
        <ThemedDatePicker
          name="due"
          label="Due date"
          defaultValue="2026-09-18"
          min="2026-09-05"
          previousMonthLabel="Previous month"
          nextMonthLabel="Next month"
          invalidDateText="Pick a date on or after {date}"
        />
      </div>
    </Opened>
  ),
};

export const RangePhone: Story = { ...Range, globals: mobile };
export const SingleDayPhone: Story = { ...SingleDay, globals: mobile };
