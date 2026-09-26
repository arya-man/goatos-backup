// One bar of the Weights page metric charts (breed/sex/stage/load/pen), resolved by the server
// page and drawn by the template AnalyticsConversionRates card in metric-chart.tsx; the shed table
// reads `shedName` + `cohorts` from the SAME row.
import type { Tone } from "@/components/ui-primitives";

export type WeightBar = {
  key: string;
  label: string;
  value: number;
  valueLabel?: string;
  modeLabel?: string;
  modeTone?: Tone;
  /**
   * Table-only companions to `label`, ignored by every bar renderer here.
   *
   * The chart has one text column, so a pen's breed/sex/head count can only ride inside
   * `label`. The shed table has real columns for them, and a mixed pen's composition ran
   * past a hundred characters when it was crammed into the name. Both views therefore read
   * the SAME row: the chart draws `label`, the table draws `shedName` plus `cohorts`, and
   * neither can disagree with the other about which pen it is showing.
   */
  shedName?: string;
  cohorts?: readonly { breed: string; sex: string; animals: number }[];
};
