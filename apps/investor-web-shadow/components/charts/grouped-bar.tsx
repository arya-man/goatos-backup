'use client';

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Legend,
  LabelList,
  ResponsiveContainer,
  Tooltip,
} from 'recharts';
import { TEXT_MUTED, TEXT_TERTIARY, pickSpacedColors } from '@/lib/constants';

const tooltipStyle = {
  backgroundColor: '#1A1D24',
  border: '1px solid #334155',
  borderRadius: 8,
  fontSize: 12,
  color: '#FFFFFF',
};

// Cool-toned palette for stacked charts with many segments
const STACKED_COLORS = [
  '#14F1D9', '#12D9C3', '#10C1AD', '#10FF98', '#20E890',
  '#30D188', '#40BA80', '#50A378', '#608C70', '#14D4E8',
  '#14B8D0', '#149CB8', '#1480A0', '#146488', '#144870',
  '#C6FF00', '#A8D900', '#8AB300', '#6C8D00', '#4E6700',
  '#1AF0B0', '#22E0A0', '#2AD090', '#32C080', '#3AB070',
];

/** Custom x-axis tick that renders multiline labels (split on \n) */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function MultiLineTick({ x, y, payload }: any) {
  const lines = String(payload.value).split('\n');
  return (
    <text x={x} y={y} textAnchor="middle" fontSize={11} fill={TEXT_MUTED}>
      {lines.map((line: string, i: number) => (
        <tspan key={i} x={x} dy={i === 0 ? 12 : 14} fill={i === 0 ? TEXT_MUTED : TEXT_TERTIARY} fontSize={i === 0 ? 11 : 10}>
          {line}
        </tspan>
      ))}
    </text>
  );
}

/** Custom x-axis tick tilted -45° for multiline labels */
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

/** Custom y-axis tick that renders multiline labels (split on \n) */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function MultiLineYTick({ x, y, payload }: any) {
  const lines = String(payload.value).split('\n');
  return (
    <text x={x} y={y} textAnchor="end" fontSize={11} fill={TEXT_MUTED} dominantBaseline="central">
      {lines.map((line: string, i: number) => (
        <tspan key={i} x={x} dy={i === 0 ? 0 : 14} fill={i === 0 ? TEXT_MUTED : TEXT_TERTIARY} fontSize={i === 0 ? 11 : 10}>
          {line}
        </tspan>
      ))}
    </text>
  );
}

interface GroupedBarChartProps {
  data: { name: string; [key: string]: string | number }[];
  keys: string[];
  height?: number;
  stacked?: boolean;
  valueSuffix?: string;
  layout?: 'vertical' | 'horizontal';
  showDataLabels?: boolean;
  tiltXLabels?: boolean;
}

export function GroupedBarChart({ data, keys, height, stacked = false, valueSuffix = '', layout = 'vertical', showDataLabels = false, tiltXLabels = false }: GroupedBarChartProps) {
  const colors = stacked ? STACKED_COLORS : pickSpacedColors(keys.length);
  const isHorizontal = layout === 'horizontal';
  const chartHeight = height ?? (isHorizontal ? Math.max(200, data.length * (keys.length * 12 + 30)) : 350);

  return (
    <ResponsiveContainer width="100%" height={chartHeight}>
      <BarChart
        data={data}
        layout={isHorizontal ? 'vertical' : 'horizontal'}
        margin={{ top: 8, right: isHorizontal ? 50 : 16, bottom: (!isHorizontal && tiltXLabels) ? 20 : 4, left: isHorizontal ? 10 : 0 }}
        barCategoryGap="20%"
        barGap={2}
      >
        {isHorizontal ? (
          <>
            <XAxis
              type="number"
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
              tickFormatter={valueSuffix ? (v) => `${v}${valueSuffix}` : undefined}
            />
            <YAxis
              type="category"
              dataKey="name"
              axisLine={false}
              tickLine={false}
              tick={<MultiLineYTick />}
              width={120}
            />
          </>
        ) : (
          <>
            <XAxis
              dataKey="name"
              axisLine={false}
              tickLine={false}
              tick={tiltXLabels ? <TiltedMultiLineTick /> : <MultiLineTick />}
              interval={tiltXLabels ? 0 : undefined}
              height={tiltXLabels ? 80 : 50}
            />
            <YAxis
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
              tickFormatter={valueSuffix ? (v) => `${v}${valueSuffix}` : undefined}
            />
          </>
        )}
        <Tooltip
          contentStyle={tooltipStyle}
          itemStyle={{ color: '#FFFFFF' }}
          labelStyle={{ color: '#14F1D9', fontSize: 11 }}
          cursor={{ fill: 'rgba(20, 241, 217, 0.03)' }}
          itemSorter={(item) => keys.indexOf(String(item.dataKey ?? ''))}
          formatter={valueSuffix ? (value: unknown) => `${value}${valueSuffix}` : undefined}
        />
        <Legend
          verticalAlign="top"
          wrapperStyle={{ fontSize: 12, paddingBottom: 8, color: '#B0BEC5' }}
          {...{ payload: keys.map((key, i) => ({
            value: key,
            type: 'rect' as const,
            color: colors[i % colors.length],
          })) }}
        />
        {keys.map((key, i) => (
          <Bar
            key={key}
            dataKey={key}
            fill={colors[i % colors.length]}
            stackId={stacked ? 'stack' : undefined}
            radius={isHorizontal
              ? (stacked ? undefined : [0, 4, 4, 0])
              : (stacked ? undefined : [4, 4, 0, 0])
            }
            animationDuration={800}
            animationEasing="ease-out"
          >
            {showDataLabels && (
              <LabelList
                dataKey={key}
                position={isHorizontal ? 'right' : 'top'}
                fontSize={10}
                fill="#E0E8F0"
                formatter={valueSuffix ? ((v: unknown) => `${v}${valueSuffix}`) as never : undefined}
              />
            )}
          </Bar>
        ))}
      </BarChart>
    </ResponsiveContainer>
  );
}
