'use client';


import { chartColor, chartRamp, type ChartColorKey, useChartTheme } from '@/components/app/chart-colors';
import { BankingExpensesCategories } from '@/components/app/sections/overview/banking/banking-expenses-categories';

type Props = React.ComponentProps<typeof BankingExpensesCategories> & {
  /** Palette channels per series, so a page can paint the SAME colour a sibling card uses for that
   * series (Farm value: the polar slices and legend match the By-category bars, TR2-P1-3). */
  colorKeys?: readonly ChartColorKey[];
};

/** The template categories donut with the locked Mesha categorical ramp as `chart.colors` (the
 * template default includes error red, which never paints an ordinary category). Client leaf so a
 * server page can render it: the ramp resolves against the active scheme. */
export function CategoriesCard({ chart, colorKeys, ...props }: Props) {
  const theme = useChartTheme();
  const colors = chart.colors ?? (colorKeys?.length ? chart.series.map((_, i) => chartColor(theme, colorKeys[i % colorKeys.length])) : chartRamp(theme));
  return <BankingExpensesCategories {...props} chart={{ ...chart, colors }} />;
}
