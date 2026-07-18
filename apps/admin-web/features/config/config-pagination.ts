export function shouldShowConfigRulePager(totalRules: number, pageSize: number): boolean {
  if (!Number.isFinite(totalRules) || !Number.isFinite(pageSize) || pageSize <= 0) return false;
  return totalRules > pageSize;
}
