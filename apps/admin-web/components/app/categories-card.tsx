'use client';

import { useTheme } from '@mui/material/styles';

import { chartRamp } from '@/components/app/chart-colors';
import { BankingExpensesCategories } from '@/components/app/sections/overview/banking/banking-expenses-categories';

type Props = React.ComponentProps<typeof BankingExpensesCategories>;

/** The template categories donut with the locked Mesha categorical ramp as `chart.colors` (the
 * template default includes error red, which never paints an ordinary category). Client leaf so a
 * server page can render it: the ramp resolves against the active scheme. */
export function CategoriesCard({ chart, ...props }: Props) {
  const theme = useTheme();
  return <BankingExpensesCategories {...props} chart={{ ...chart, colors: chart.colors ?? chartRamp(theme) }} />;
}
