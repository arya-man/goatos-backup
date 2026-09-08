// Pure helpers for the Notifications matrix (maintainer decision 2026-09-08), kept out of the
// component so they are unit-testable without React: grouping rows under their module in the
// backend's order, and the draft/dirty arithmetic for one row's ticks.

export type MatrixAlert = {
  key: string;
  module: string;
  module_label: string;
  label: string;
  blurb: string;
  default_designations: string[];
  designations: string[];
  customised: boolean;
  row_version: number;
};

export type MatrixModule = { key: string; label: string };

export type ModuleGroup = { key: string; label: string; alerts: MatrixAlert[] };

/**
 * Group alerts under the backend's module order. A module with no alert is dropped (an empty
 * heading is noise); an alert whose module the backend did not list is still shown, under its own
 * module_label, rather than silently vanishing.
 */
export function groupAlertsByModule(modules: MatrixModule[], alerts: MatrixAlert[]): ModuleGroup[] {
  const order = new Map<string, ModuleGroup>();
  for (const m of modules) order.set(m.key, { key: m.key, label: m.label, alerts: [] });
  for (const alert of alerts) {
    let group = order.get(alert.module);
    if (!group) {
      group = { key: alert.module, label: alert.module_label || alert.module, alerts: [] };
      order.set(alert.module, group);
    }
    group.alerts.push(alert);
  }
  return [...order.values()].filter((g) => g.alerts.length > 0);
}

/** Toggle one designation on a draft, returning a NEW sorted list. */
export function toggleDesignation(draft: string[], code: string): string[] {
  const next = draft.includes(code) ? draft.filter((c) => c !== code) : [...draft, code];
  return [...next].sort();
}

/** Same set, order-insensitive. */
export function sameDesignations(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sa = [...a].sort();
  const sb = [...b].sort();
  return sa.every((v, i) => v === sb[i]);
}

/** A row is dirty when its draft differs from what the backend last returned. */
export function isDirty(alert: MatrixAlert, draft: string[]): boolean {
  return !sameDesignations(alert.designations, draft);
}
