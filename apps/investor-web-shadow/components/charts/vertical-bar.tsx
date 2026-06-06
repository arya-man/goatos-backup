'use client';

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Cell,
  ResponsiveContainer,
  LabelList,
  Tooltip,
  Legend,
} from 'recharts';
import { assignBarShades, TEXT_MUTED, TEXT_TERTIARY } from '@/lib/constants';

const tooltipStyle = {
  backgroundColor: '#1A1D24',
  border: '1px solid #334155',
  borderRadius: 8,
  fontSize: 12,
  color: '#FFFFFF',
};

interface VerticalBarChartProps {
  data: { name: string; value: number }[];
  height?: number;
  valueFormatter?: (v: number) => string;
  compact?: boolean;
  valueLabel?: string;
}

export function VerticalBarChart({ data, height = 300, valueFormatter, compact, valueLabel }: VerticalBarChartProps) {
  const shaded = assignBarShades(data);
  const isCompact = compact ?? data.length <= 3;
  const barWidth = isCompact ? 48 : 32;

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart
        data={shaded}
        margin={{ top: 40, right: 16, bottom: 4, left: 0 }}
        barCategoryGap="20%"
      >
        <XAxis
          dataKey="name"
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
          contentStyle={tooltipStyle}
          itemStyle={{ color: '#FFFFFF' }}
          labelStyle={{ color: '#14F1D9', fontSize: 11 }}
          cursor={{ fill: 'rgba(20, 241, 217, 0.03)' }}
        />
        <Legend verticalAlign="bottom" wrapperStyle={{ fontSize: 12, color: '#B0BEC5' }} />
        <Bar
          dataKey="value"
          name={valueLabel}
          radius={[4, 4, 0, 0]}
          barSize={barWidth}
          maxBarSize={isCompact ? 60 : undefined}
          animationDuration={800}
          animationEasing="ease-out"
        >
          {shaded.map((entry, index) => (
            <Cell key={`cell-${index}`} fill={entry.fill} />
          ))}
          <LabelList
            dataKey="value"
            position="top"
            fontSize={12}
            fill="#E0E8F0"
            formatter={valueFormatter as never}
          />
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}
