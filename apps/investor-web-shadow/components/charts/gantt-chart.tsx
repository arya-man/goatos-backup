'use client';

import { cn } from '@/lib/utils';
import { CROP_COLORS } from '@/lib/display-utils';

interface GanttRow {
  region: string;
  crop: string;
  startMonth: number;
  endMonth: number;
}

interface GanttChartProps {
  data: GanttRow[];
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function GanttChart({ data }: GanttChartProps) {
  const currentMonth = new Date().getMonth(); // 0-indexed

  // Group by region
  const grouped: Record<string, GanttRow[]> = {};
  for (const row of data) {
    if (!grouped[row.region]) grouped[row.region] = [];
    grouped[row.region].push(row);
  }

  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full min-w-[700px] border-collapse text-xs">
        <thead>
          <tr>
            <th className="w-[140px] px-2 py-2 text-left text-[10px] font-medium uppercase tracking-wider text-[#B0BEC5]">
              Crop
            </th>
            {MONTHS.map((m, i) => (
              <th
                key={m}
                className={cn(
                  "px-1 py-2 text-center text-[10px] font-medium uppercase tracking-wider text-[#B0BEC5]",
                  i === currentMonth && "bg-[#22262E]"
                )}
              >
                {m}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Object.entries(grouped).map(([region, rows]) => (
            <>
              <tr key={`region-${region}`}>
                <td
                  colSpan={13}
                  className="px-2 pt-4 pb-1 text-[11px] font-bold uppercase tracking-wider text-[#B0BEC5]"
                >
                  {region}
                </td>
              </tr>
              {rows.map((row, rowIdx) => {
                const shade = CROP_COLORS[row.crop] || '#14F1D9';
                return (
                  <tr key={`${region}-${row.crop}-${rowIdx}`} className="group">
                    <td className="px-2 py-1 text-[11px] text-[#B0BEC5]">
                      {row.crop}
                    </td>
                    {MONTHS.map((_m, monthIdx) => {
                      const inRange =
                        row.startMonth <= row.endMonth
                          ? monthIdx >= row.startMonth && monthIdx <= row.endMonth
                          : monthIdx >= row.startMonth || monthIdx <= row.endMonth;

                      const isStart =
                        row.startMonth <= row.endMonth
                          ? monthIdx === row.startMonth
                          : monthIdx === row.startMonth;

                      return (
                        <td
                          key={monthIdx}
                          className={cn(
                            "relative h-7 px-0 py-0.5 overflow-visible",
                            monthIdx === currentMonth && "bg-[#22262E]"
                          )}
                        >
                          {inRange && (
                            <div
                              className={cn(
                                "h-5 w-full",
                                isStart && "rounded-l",
                                monthIdx === row.endMonth && "rounded-r"
                              )}
                              style={{ backgroundColor: shade }}
                              title={row.crop}
                            >
                              {isStart && (
                                <span className="absolute inset-0 flex items-center pl-1.5 text-[10px] font-medium text-white whitespace-nowrap overflow-visible z-10 drop-shadow-[0_1px_1px_rgba(0,0,0,0.5)]">
                                  {row.crop}
                                </span>
                              )}
                            </div>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </>
          ))}
        </tbody>
      </table>
      {/* Legend */}
      <div className="flex flex-wrap gap-3 mt-4 px-2">
        {Object.entries(CROP_COLORS).map(([crop, color]) => (
          <div key={crop} className="flex items-center gap-1.5">
            <div className="w-3 h-3 rounded-sm shrink-0" style={{ backgroundColor: color }} />
            <span className="text-[10px] text-[#B0BEC5]">{crop}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
