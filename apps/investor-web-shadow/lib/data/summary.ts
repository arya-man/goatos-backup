// ── Summary mock data ──

export interface SummaryPeriod {
  births: number;
  deaths: number;
  purchases: number;
  sales: number;
  abortions: number;
}

export function getDailySummary(): SummaryPeriod {
  return { births: 8, deaths: 0, purchases: 47, sales: 0, abortions: 0 };
}

export function getWeeklySummary(): SummaryPeriod {
  return { births: 36, deaths: 1, purchases: 47, sales: 42, abortions: 0 };
}

export function getMonthlySummary(): SummaryPeriod {
  return { births: 76, deaths: 10, purchases: 47, sales: 57, abortions: 1 };
}
