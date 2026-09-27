// Filter control width floors, shared by the controls themselves (WorklistFilters, DateRangePicker)
// and the loading twins that stand in for them (features/**/*-layout.ts), so a skeleton's filter row
// cannot drift from the bar it twins. Plain module (no "use client"): server skeletons import it too.
/** A WorklistFilters select / multi-select, from sm. */
export const FILTER_SELECT_MIN = 160;
/** A WorklistFilters single DatePicker, from sm. */
export const FILTER_DATE_MIN = 180;
/** The DateRangePicker trigger field, from sm. */
export const DATE_RANGE_PICKER_MIN = 300;
