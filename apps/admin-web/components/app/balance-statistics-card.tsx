'use client';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardHeader from '@mui/material/CardHeader';

import { BankingBalanceStatistics } from '@/components/app/sections/overview/banking/banking-balance-statistics';


type Props = React.ComponentProps<typeof BankingBalanceStatistics> & { empty?: React.ReactNode };

/** The template balance-statistics card, or (no categories in the first series) the same Card +
 * CardHeader with the page's empty state, so the template file never grows an empty branch. */
export function BalanceStatisticsCard({ empty, ...props }: Props) {
  if ((props.chart.series[0]?.categories.length ?? 0) === 0) {
    return (
      <Card sx={props.sx}>
        <CardHeader title={props.title} subheader={props.subheader} sx={{ mb: 3 }} />
        <Box sx={{ p: 3 }}>{empty}</Box>
        {props.children}
      </Card>
    );
  }
  return <BankingBalanceStatistics {...props} />;
}
