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
import { assignBarShades, TEXT_MUTED } from '@/lib/constants';

const tooltipStyle = {
  backgroundColor: '#1A1D24',
  border: '1px solid #334155',
  borderRadius: 8,
  fontSize: 12,
  color: '#FFFFFF',
};

interface HorizontalBarChartProps {
  data: { name: string; value: number }[];
  height?: number;
  valueLabel?: string;
  showLabels?: boolean;
  yAxisWidth?: number;
}

export function HorizontalBarChart({ data, height, valueLabel, showLabels = true, yAxisWidth = 136 }: HorizontalBarChartProps) {
  const shaded = assignBarShades(data);
  const chartHeight = height ?? Math.max(168, data.length * 52 + 52);

  return (
    <ResponsiveContainer width="100%" height={chartHeight}>
      <BarChart
        data={shaded}
        layout="vertical"
        margin={{ top: 8, right: 58, bottom: 8, left: 0 }}
        barCategoryGap="34%"
      >
        <XAxis type="number" hide />
        <YAxis
          type="category"
          dataKey="name"
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 10, fill: TEXT_MUTED }}
          interval={0}
          width={yAxisWidth}
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
          radius={[0, 4, 4, 0]}
          barSize={22}
          animationDuration={0}
        >
          {shaded.map((entry, index) => (
            <Cell key={`cell-${index}`} fill={entry.fill} />
          ))}
          {showLabels && (
            <LabelList
              dataKey="value"
              position="right"
              fontSize={12}
              fill="#E0E8F0"
              formatter={(value) => (typeof value === "number" ? value.toLocaleString("en-IN") : String(value ?? ""))}
            />
          )}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}
