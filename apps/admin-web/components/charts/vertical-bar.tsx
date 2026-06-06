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

/** Custom x-axis tick tilted -45 degrees for multiline labels */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function TiltedMultiLineTick({ x, y, payload }: any) {
  const lines = String(payload.value).split('\n');
  return (
    <g transform={`translate(${x},${y})`}>
      <text transform="rotate(-45)" textAnchor="end" fontSize={11} fill={TEXT_MUTED}>
        {lines.map((line: string, i: number) => (
          <tspan key={i} x={0} dy={i === 0 ? 12 : 14} fill={i === 0 ? TEXT_MUTED : TEXT_TERTIARY} fontSize={i === 0 ? 11 : 10}>
            {line}
          </tspan>
        ))}
      </text>
    </g>
  );
}

interface VerticalBarChartProps {
  data: { name: string; value: number }[];
  height?: number;
  valueFormatter?: (v: number) => string;
  yAxisFormatter?: (v: number) => string;
  compact?: boolean;
  valueLabel?: string;
  showLabels?: boolean;
  tiltXLabels?: boolean;
}

export function VerticalBarChart({ data, height = 300, valueFormatter, yAxisFormatter, compact, valueLabel, showLabels = true, tiltXLabels = false }: VerticalBarChartProps) {
  const shaded = assignBarShades(data);
  const isCompact = compact ?? data.length <= 3;
  const barWidth = isCompact ? 48 : 32;

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart
        data={shaded}
        margin={{ top: 40, right: 16, bottom: tiltXLabels ? 20 : 4, left: 0 }}
        barCategoryGap="20%"
      >
        <XAxis
          dataKey="name"
          axisLine={false}
          tickLine={false}
          tick={tiltXLabels ? <TiltedMultiLineTick /> : { fontSize: 11, fill: TEXT_MUTED }}
          interval={tiltXLabels ? 0 : undefined}
          height={tiltXLabels ? 80 : undefined}
        />
        <YAxis
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
          tickFormatter={yAxisFormatter as never}
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
          {showLabels && (
            <LabelList
              dataKey="value"
              position="top"
              fontSize={12}
              fill="#E0E8F0"
              formatter={valueFormatter as never}
            />
          )}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}
