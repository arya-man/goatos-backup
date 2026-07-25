// Backend-owned park-scope vocabulary.
//
// BUG-019: a caller whose authorized scope spans more than one park cannot be given a park by the
// frontend — blending several parks' rosters into one screen is the original defect, and picking one
// in React would be the frontend inventing product truth. The backend refuses with 409
// `park_scope_ambiguous` and returns the parks the caller may choose from, compiled from canonical
// Postgres `locations` rows. This module carries that contract across the thin admin-web passthrough
// so a page can render a selector instead of dying on a thrown error.

import type { AppApiComponents } from '@goatos/api-client';

export type ParkScopeOption = AppApiComponents['schemas']['VaccinationParkScopeOption'];

export const PARK_SCOPE_AMBIGUOUS_CODE = 'park_scope_ambiguous';
export const OPERATOR_ASSIGNMENT_CONFIG_NOT_FOUND_CODE = 'not_found';

/** Thrown by the client passthrough for a 409 `park_scope_ambiguous`. Carries the backend's options. */
export class ParkScopeAmbiguousError extends Error {
  readonly code = PARK_SCOPE_AMBIGUOUS_CODE;
  readonly availableParks: ParkScopeOption[];

  constructor(message: string, availableParks: ParkScopeOption[]) {
    super(message);
    this.name = 'ParkScopeAmbiguousError';
    this.availableParks = availableParks;
  }
}

/** Narrow an unknown error body from the route passthrough into the ambiguous-scope case. */
export function parkScopeAmbiguousFromBody(status: number, body: unknown): ParkScopeAmbiguousError | null {
  if (status !== 409 || typeof body !== 'object' || body === null) return null;
  const envelope = body as { code?: unknown; message?: unknown; availableParks?: unknown };
  if (envelope.code !== PARK_SCOPE_AMBIGUOUS_CODE) return null;
  const parks = Array.isArray(envelope.availableParks) ? (envelope.availableParks as ParkScopeOption[]) : [];
  const message = typeof envelope.message === 'string' ? envelope.message : 'Choose a park to continue.';
  return new ParkScopeAmbiguousError(message, parks);
}

/** Thrown by the client passthrough when a park has no authored operator-assignment config yet. */
export class OperatorAssignmentConfigNotFoundError extends Error {
  readonly code = OPERATOR_ASSIGNMENT_CONFIG_NOT_FOUND_CODE;

  constructor(message = 'No operator assignment config authored for this park yet.') {
    super(message);
    this.name = 'OperatorAssignmentConfigNotFoundError';
  }
}

/** Narrow an unknown error body into the no-authored-config case. */
export function operatorAssignmentConfigNotFoundFromBody(status: number, body: unknown): OperatorAssignmentConfigNotFoundError | null {
  if (status !== 404 || typeof body !== 'object' || body === null) return null;
  const envelope = body as { code?: unknown; message?: unknown };
  if (envelope.code !== OPERATOR_ASSIGNMENT_CONFIG_NOT_FOUND_CODE) return null;
  const message = typeof envelope.message === 'string' ? envelope.message : undefined;
  return new OperatorAssignmentConfigNotFoundError(message);
}
