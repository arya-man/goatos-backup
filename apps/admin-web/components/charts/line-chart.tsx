'use client';

import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Dot,
} from 'recharts';
import { AREA_ACCENT, TEXT_MUTED, TEXT_TERTIARY } from '@/lib/constants';

interface LineChartProps {
  data: { date: string; value: number }[];
  height?: number;
  showArea?: boolean;
  accentColor?: string;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function LastDot(props: any) {
  const { cx, cy, index, dataLength, accent } = props;
  if (index !== dataLength - 1) return null;
  return (
    <Dot cx={cx} cy={cy} r={4} fill={accent} stroke="#0F1115" strokeWidth={2} />
  );
}

export function LineChart({
  data,
  height = 280,
  showArea = true,
  accentColor,
}: LineChartProps) {
  const accent = accentColor ?? AREA_ACCENT;
  const gradientId = 'lineChartGradient';

  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart
        data={data}
        margin={{ top: 8, right: 16, bottom: 4, left: 0 }}
      >
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={accent} stopOpacity={0.15} />
            <stop offset="100%" stopColor={accent} stopOpacity={0} />
          </linearGradient>
        </defs>
        <XAxis
          dataKey="date"
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 11, fill: TEXT_MUTED }}
        />
        <YAxis
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#1A1D24',
            border: '1px solid #334155',
            borderRadius: 8,
            fontSize: 12,
            color: '#FFFFFF',
          }}
          itemStyle={{ color: '#FFFFFF' }}
          labelStyle={{ color: '#14F1D9', fontSize: 11 }}
        />
        <Area
          type="monotone"
          dataKey="value"
          stroke={accent}
          strokeWidth={2}
          fill={showArea ? `url(#${gradientId})` : 'none'}
          animationDuration={800}
          animationEasing="ease-out"
          dot={(dotProps) => (
            <LastDot
              {...dotProps}
              dataLength={data.length}
              accent={accent}
            />
          )}
          activeDot={{ r: 5, fill: accent, stroke: '#0F1115', strokeWidth: 2 }}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}
