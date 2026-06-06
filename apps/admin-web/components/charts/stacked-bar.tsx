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

interface StackedBarChartProps {
  data: { name: string; [key: string]: string | number }[];
  keys: string[];
  layout?: 'vertical' | 'horizontal';
  height?: number;
  showSegmentLabels?: boolean;
  showLabels?: boolean;
  colors?: string[];
}

export function StackedBarChart({
  data,
  keys,
  layout = 'vertical',
  height = 350,
  showSegmentLabels = false,
  showLabels = true,
  colors,
}: StackedBarChartProps) {
  const isHorizontal = layout === 'horizontal';
  const chartHeight = isHorizontal ? Math.max(200, data.length * 44) : height;
  const palette = colors ?? pickSpacedColors(keys.length);

  return (
    <ResponsiveContainer width="100%" height={chartHeight}>
      <BarChart
        data={data}
        layout={isHorizontal ? 'vertical' : 'horizontal'}
        margin={{
          top: 8,
          right: 16,
          bottom: 4,
          left: isHorizontal ? 80 : 0,
        }}
        barCategoryGap="20%"
      >
        {isHorizontal ? (
          <>
            <XAxis
              type="number"
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
            />
            <YAxis
              type="category"
              dataKey="name"
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_MUTED }}
              width={76}
            />
          </>
        ) : (
          <>
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
          </>
        )}
        <Tooltip
          contentStyle={tooltipStyle}
          itemStyle={{ color: '#FFFFFF' }}
          labelStyle={{ color: '#14F1D9', fontSize: 11 }}
          cursor={{ fill: 'rgba(20, 241, 217, 0.03)' }}
        />
        <Legend
          verticalAlign="top"
          wrapperStyle={{ fontSize: 12, paddingBottom: 8, color: '#B0BEC5' }}
        />
        {keys.map((key, i) => {
          const isLast = i === keys.length - 1;
          let radius: [number, number, number, number] = [0, 0, 0, 0];
          if (isHorizontal && isLast) {
            radius = [0, 4, 4, 0];
          } else if (!isHorizontal && isLast) {
            radius = [4, 4, 0, 0];
          }
          return (
            <Bar
              key={key}
              dataKey={key}
              stackId="stack"
              fill={(palette)[i % (palette).length]}
              radius={radius}
              animationDuration={800}
              animationEasing="ease-out"
            >
              {showLabels && showSegmentLabels && (
                <LabelList
                  dataKey={key}
                  position={isHorizontal ? "center" : "center"}
                  fontSize={10}
                  fill="#E0E8F0"
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
                  formatter={(v: any) => (Number(v) ? Number(v).toFixed(1) : '')}
                />
              )}
              {showLabels && !showSegmentLabels && isLast && (
                <LabelList
                  position={isHorizontal ? "right" : "top"}
                  // eslint-disable-next-line @typescript-eslint/no-unused-vars, @typescript-eslint/no-explicit-any
                  content={({ x, y, width, height, value: _v, ...rest }: any) => {
                    // eslint-disable-next-line @typescript-eslint/no-explicit-any
                    const entry = (rest as any).payload || {};
                    const total = keys.reduce((sum, k) => sum + (Number(entry[k]) || 0), 0);
                    if (isHorizontal) {
                      return (
                        <text x={(x as number) + (width as number) + 4} y={(y as number) + (height as number) / 2} fill="#E0E8F0" fontSize={11} dominantBaseline="middle">
                          {total.toFixed(1)}
                        </text>
                      );
                    }
                    return (
                      <text x={(x as number) + (width as number) / 2} y={(y as number) - 6} fill="#E0E8F0" fontSize={11} textAnchor="middle">
                        {total.toFixed(1)}
                      </text>
                    );
                  }}
                />
              )}
            </Bar>
          );
        })}
      </BarChart>
    </ResponsiveContainer>
  );
}
