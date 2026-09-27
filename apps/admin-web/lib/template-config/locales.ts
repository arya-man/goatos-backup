// Template config point for src/locales `formatNumberLocale` (the template reads it from i18next).
// Goat OS is single-locale: Indian digit grouping and rupees. Read by the verbatim template
// src/utils/format-number.ts copy (components/minimal/_shared/format-number.ts).
export function formatNumberLocale() {
  return { code: 'en-IN', currency: 'INR' };
}
