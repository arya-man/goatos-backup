'use client';

import {
  PieChart as RechartsPieChart,
  Pie,
  Cell,
  Legend,
  ResponsiveContainer,
  Tooltip,
} from 'recharts';
import { pickSpacedColors } from '@/lib/constants';

const tooltipStyle = {
  backgroundColor: '#1A1D24',
  border: '1px solid #334155',
  borderRadius: 8,
  fontSize: 12,
  color: '#FFFFFF',
};

interface PieChartProps {
  data: { name: string; value: number }[];
  height?: number;
  showPercentInLegend?: boolean;
}

export function PieChart({ data, height = 280, showPercentInLegend }: PieChartProps) {
  const total = data.reduce((s, d) => s + d.value, 0);
  const pieColors = pickSpacedColors(data.length);

  const legendFormatter = (value: string) => {
    if (!showPercentInLegend) return value;
    const item = data.find((d) => d.name === value);
    if (!item || total === 0) return value;
    const pct = ((item.value / total) * 100).toFixed(1);
    return `${value} (${pct}%)`;
  };

  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unused-vars
  const renderCustomLabel = ({ name, percent, x: _x, y: _y, midAngle, innerRadius: _ir, outerRadius, cx, cy }: any) => {
    if (percent < 0.10) return null; // hide labels for slices < 10%
    const RADIAN = Math.PI / 180;
    const radius = outerRadius * 1.15;
    const lx = cx + radius * Math.cos(-midAngle * RADIAN);
    const ly = cy + radius * Math.sin(-midAngle * RADIAN);
    const pct = (percent * 100).toFixed(1);
    return (
      <text x={lx} y={ly} fill="#E0E8F0" textAnchor={lx > cx ? 'start' : 'end'} dominantBaseline="central" fontSize={11}>
        {`${name} ${pct}%`}
      </text>
    );
  };

  return (
    <ResponsiveContainer width="100%" height={height}>
      <RechartsPieChart>
        <Pie
          data={data}
          dataKey="value"
          nameKey="name"
          cx="50%"
          cy="50%"
          outerRadius="75%"
          strokeWidth={0}
          animationDuration={800}
          animationEasing="ease-out"
          label={renderCustomLabel}
        >
          {data.map((_entry, index) => (
            <Cell
              key={`cell-${index}`}
              fill={pieColors[index % pieColors.length]}
            />
          ))}
        </Pie>
        <Tooltip
          contentStyle={tooltipStyle}
          itemStyle={{ color: '#FFFFFF' }}
          labelStyle={{ color: '#14F1D9', fontSize: 11 }}
          formatter={(value) => [typeof value === 'number' ? Number(value.toFixed(2)) : value, undefined]}
        />
        <Legend
          verticalAlign="bottom"
          wrapperStyle={{ fontSize: 12, color: '#B0BEC5' }}
          formatter={legendFormatter}
        />
      </RechartsPieChart>
    </ResponsiveContainer>
  );
}
