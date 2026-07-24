// Park-scope resolution + one-shot data load for the Vaccination operators screen.
//
// BUG-019. Park scope is BACKEND-owned end to end:
//   * the first read omits park_id and lets the backend resolve the caller's scope;
//   * a caller whose scope already resolves to one park is answered with that park (zero clicks);
//   * a caller whose scope covers several parks gets a 409 carrying the parks they may choose from,
//     which this module turns into a `needs_park_selection` state instead of a thrown dead end. The
//     maintainer's own ceo_internal TENANT-scoped account is exactly that caller;
//   * nothing is auto-picked, and no downstream read runs until one park is settled — the original
//     defect was a roster/KPI/preview/dropdown blended across every park of the tenant.
//
// Extracted from the screen's effect so the four rules above are testable without a DOM
// (features/people/vaccination-operators-scope.test.mjs).

import type { ParkScopeOption } from '@/lib/api/park-scope';

// Re-declared (not imported) so this module has no runtime import and stays unit-testable under plain
// `node --test`. The annotation is a TYPE-ONLY import, so tsc still fails the build if this literal
// ever drifts from the single source in lib/api/park-scope.ts.
const PARK_SCOPE_AMBIGUOUS_CODE: typeof import('@/lib/api/park-scope').PARK_SCOPE_AMBIGUOUS_CODE = 'park_scope_ambiguous';

// Structural check rather than `instanceof ParkScopeAmbiguousError`: this module is deliberately free
// of a runtime import so it stays unit-testable under plain `node --test` (the `@/` alias above is
// type-only and erased). The code + availableParks pair IS the contract; see lib/api/park-scope.ts.
function ambiguousParkScope(err: unknown): { availableParks: ParkScopeOption[]; message: string } | null {
  if (typeof err !== 'object' || err === null) return null;
  const candidate = err as { code?: unknown; message?: unknown; availableParks?: unknown };
  if (candidate.code !== PARK_SCOPE_AMBIGUOUS_CODE || !Array.isArray(candidate.availableParks)) return null;
  return {
    availableParks: candidate.availableParks as ParkScopeOption[],
    message: typeof candidate.message === 'string' ? candidate.message : 'Choose a park to continue.',
  };
}

export interface VaccinationOperatorsScreenApi {
  getVaccinationOperatorAssignmentConfig(parkId?: string): Promise<{ data?: unknown }>;
  listStaffPositions(params: Record<string, unknown>): Promise<{ data?: { items?: unknown[] } }>;
  getVaccinationCapacityConfig(): Promise<{ data?: { maxPerDay?: number } }>;
  listStaffLeave(params?: Record<string, unknown>): Promise<{ data?: { items?: unknown[] } }>;
}

export type VaccinationOperatorsScreenData =
  | { state: 'needs_park_selection'; parks: ParkScopeOption[]; message: string }
  | {
      state: 'ready';
      parkId: string;
      config: Record<string, unknown> | null;
      positions: unknown[];
      commonCap: number;
      leaveItems: unknown[];
    };

export async function loadVaccinationOperatorsScreen(
  api: VaccinationOperatorsScreenApi,
  chosenParkId?: string,
): Promise<VaccinationOperatorsScreenData> {
  let configRes: { data?: unknown };
  try {
    configRes = await api.getVaccinationOperatorAssignmentConfig(chosenParkId);
  } catch (err) {
    const ambiguous = ambiguousParkScope(err);
    if (ambiguous) {
      // Backend-owned options + backend-owned reason text. The screen renders a selector; it does
      // not compose its own park list, labels, or copy, and it picks nothing on the caller's behalf.
      return { state: 'needs_park_selection', parks: ambiguous.availableParks, message: ambiguous.message };
    }
    throw err;
  }

  const config = (configRes.data as Record<string, unknown> | undefined) ?? null;
  const resolvedParkId = (config?.parkId as string | undefined) ?? chosenParkId ?? null;
  if (!resolvedParkId) {
    throw new Error('Park scope unavailable for this account. The roster cannot be shown without a single resolved park.');
  }

  const [posRes, capRes, leaveRes] = await Promise.all([
    api.listStaffPositions({ status: 'active', scope_type: 'center', scope_id: resolvedParkId, limit: 500 }),
    api.getVaccinationCapacityConfig().catch(() => ({ data: { maxPerDay: 200 } })),
    api.listStaffLeave({ limit: 500 }).catch(() => ({ data: { items: [] } })),
  ]);

  return {
    state: 'ready',
    parkId: resolvedParkId,
    config,
    positions: posRes.data?.items ?? [],
    commonCap: capRes.data?.maxPerDay ?? 200,
    leaveItems: leaveRes.data?.items ?? [],
  };
}
