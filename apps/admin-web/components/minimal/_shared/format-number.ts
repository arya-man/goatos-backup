// Adapted from the licensed MUI Minimal template (src/utils/format-number.ts), en-IN grouping.
export function fNumber(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return value.toLocaleString('en-IN', { maximumFractionDigits: 2 });
}

export function fPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${(value).toLocaleString('en-IN', { maximumFractionDigits: 1 })}%`;
}

export function fShortenNumber(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return new Intl.NumberFormat('en-IN', { notation: 'compact', maximumFractionDigits: 1 }).format(value);
}
