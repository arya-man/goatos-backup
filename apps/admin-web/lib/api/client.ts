// Client-side API client for making requests from 'use client' components.
// This is a thin wrapper around fetch that calls the backend admin API endpoints.

import type { AdminApiComponents, AdminApiPaths, AppApiComponents } from '@goatos/api-client';
import { operatorAssignmentConfigNotFoundFromBody, parkScopeAmbiguousFromBody } from './park-scope';

/**
 * getAdminApi returns a client-side API object that can fetch roster and other admin endpoints.
 * It automatically includes the bearer token from the page context.
 */
export function getAdminApi() {
  return {
    async listStaffPositions(
      params?: AdminApiPaths['/admin/roster/positions']['get']['parameters']['query']
    ) {
      const query = new URLSearchParams();
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          if (v !== undefined && v !== null) {
            query.append(k, String(v));
          }
        });
      }
      const url = `/api/admin/roster/positions${query.toString() ? '?' + query.toString() : ''}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch positions: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['PositionListResponse'];
      return { data: body };
    },

    async listBackupConfig(
      params?: AdminApiPaths['/admin/roster/backup-config']['get']['parameters']['query']
    ) {
      const query = new URLSearchParams();
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          if (v !== undefined && v !== null) {
            query.append(k, String(v));
          }
        });
      }
      const url = `/api/admin/roster/backup-config${query.toString() ? '?' + query.toString() : ''}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch backup config: ${response.statusText}`);
      }
      const body = (await response.json()) as {
        items: AdminApiComponents['schemas']['BackupConfig'][];
        trace_id: string;
      };
      return { data: body };
    },

    async listCoverage(
      params?: AdminApiPaths['/admin/roster/coverage']['get']['parameters']['query']
    ) {
      const query = new URLSearchParams();
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          if (v !== undefined && v !== null) {
            query.append(k, String(v));
          }
        });
      }
      const url = `/api/admin/roster/coverage${query.toString() ? '?' + query.toString() : ''}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch coverage: ${response.statusText}`);
      }
      const body = (await response.json()) as {
        items: AdminApiComponents['schemas']['Coverage'][];
        trace_id: string;
      };
      return { data: body };
    },

    async getVaccinationCapacityConfig() {
      const response = await fetch('/api/vaccination/capacity-config', { cache: 'no-store' });
      if (!response.ok) {
        throw new Error(`Failed to fetch vaccination capacity config: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationCapacityConfig'];
      return { data: body };
    },

    // parkId is OPTIONAL: omitting it lets the BACKEND resolve the caller's park
    // scope and echo it back as `parkId` (BUG-019). Clients scope park-dependent
    // reads to that resolved value instead of inferring a park from row data.
    async getVaccinationOperatorAssignmentConfig(parkId?: string) {
      const query = new URLSearchParams(parkId ? { park_id: parkId } : {});
      const suffix = query.toString() ? `?${query.toString()}` : '';
      const response = await fetch(`/api/vaccination/operator-assignment/config${suffix}`, { cache: 'no-store' });
      if (!response.ok) {
        // A 409 park_scope_ambiguous is NOT a failure — it is the backend handing back the park
        // vocabulary this caller must choose from. Surface it as a typed error so the screen can
        // render the backend-owned selector instead of dead-ending on a thrown message.
        const ambiguous = parkScopeAmbiguousFromBody(response.status, await response.clone().json().catch(() => null));
        if (ambiguous) throw ambiguous;
        const notFound = operatorAssignmentConfigNotFoundFromBody(response.status, await response.clone().json().catch(() => null));
        if (notFound) throw notFound;
        throw new Error(`Failed to fetch vaccination operator assignment config: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorAssignmentConfig'];
      return { data: body };
    },

    async putVaccinationCapacityConfig(requestBody: AppApiComponents['schemas']['UpdateVaccinationCapacityConfigRequest']) {
      const response = await fetch('/api/vaccination/capacity-config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
        cache: 'no-store',
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message ?? `Failed to update vaccination capacity config: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationCapacityConfig'];
      return { data: body };
    },

    async putVaccinationOperatorAssignmentConfig(requestBody: AppApiComponents['schemas']['UpdateVaccinationOperatorAssignmentConfigRequest']) {
      const response = await fetch('/api/vaccination/operator-assignment/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
        cache: 'no-store',
      });
      if (!response.ok) {
        throw new Error(`Failed to update vaccination operator assignment config: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorAssignmentConfig'];
      return { data: body };
    },

    // A park's authored vaccination operator shifts (listed even before the park has a
    // drive-operator assignment, so a new park can set its operators up first).
    async listVaccinationOperatorShifts(parkId: string) {
      const response = await fetch(`/api/vaccination/operator-shifts?park_id=${encodeURIComponent(parkId)}`, { cache: 'no-store' });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message ?? `Failed to load operator shifts: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorShiftList'];
      return { data: body };
    },

    // Set (create or replace) one operator's shift for a park. The backend refuses a bad field
    // with a farm-worded message, which is thrown as-is so the form can show it. A fresh
    // Idempotency-Key is minted per attempt unless the caller passes one to make a retry safe.
    async putVaccinationOperatorShift(
      requestBody: AppApiComponents['schemas']['PutVaccinationOperatorShiftRequest'],
      idempotencyKey: string = crypto.randomUUID()
    ) {
      const response = await fetch('/api/vaccination/operator-shifts', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(requestBody),
        cache: 'no-store',
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message ?? `Failed to save the shift: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorShiftWriteResult'];
      return { data: body };
    },

    // Clear one operator's shift for a park. Refused while the park's drive-operator assignment
    // still names that operator; the backend's reason is thrown as-is.
    async deleteVaccinationOperatorShift(parkId: string, operatorId: string, idempotencyKey: string = crypto.randomUUID()) {
      const query = new URLSearchParams({ park_id: parkId, operator_id: operatorId });
      const response = await fetch(`/api/vaccination/operator-shifts?${query.toString()}`, {
        method: 'DELETE',
        headers: { 'Idempotency-Key': idempotencyKey },
        cache: 'no-store',
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message ?? `Failed to clear the shift: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorShiftClearResult'];
      return { data: body };
    },

    // getStaffPositionProfile backs the People position row-click drawer: the
    // enriched seat plus its holder's currently-active coverage window.
    async getStaffPositionProfile(positionId: string) {
      const response = await fetch(`/api/admin/roster/positions/${encodeURIComponent(positionId)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch position profile: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['PositionProfileResponse'];
      return { data: body };
    },

    // updateStaffPosition edits a seat's attributes in place (not its holder).
    // Optimistically locked on row_version; idempotent via the Idempotency-Key
    // header (a fresh key is minted per attempt unless the caller supplies one
    // to make a retry safe).
    async updateStaffPosition(
      positionId: string,
      requestBody: AdminApiComponents['schemas']['UpdatePositionRequest'],
      idempotencyKey: string = crypto.randomUUID()
    ) {
      const response = await fetch(`/api/admin/roster/positions/${encodeURIComponent(positionId)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        throw new Error(`Failed to update position: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['PositionResponse'];
      return { data: body };
    },

    // upsertBackupConfig backs the People "Configure backup" action: assign or
    // replace the holder of a backup-slot seat for a backup group at a scope.
    async upsertBackupConfig(
      requestBody: AdminApiComponents['schemas']['UpsertBackupConfigRequest'],
      idempotencyKey: string = crypto.randomUUID()
    ) {
      const response = await fetch('/api/admin/roster/backup-config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        throw new Error(`Failed to configure backup: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['PositionResponse'];
      return { data: body };
    },

    // importStaffPositions backs the Timetable "Import sheet" action: bulk
    // create/replace seats from already-parsed rows. The batch key makes a
    // retried import safe (each row is deduplicated by key + row index).
    async importStaffPositions(
      requestBody: AdminApiComponents['schemas']['ImportPositionsRequest'],
      idempotencyKey: string = crypto.randomUUID()
    ) {
      const response = await fetch('/api/admin/roster/positions/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        throw new Error(`Failed to import positions: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['ImportPositionsResponse'];
      return { data: body };
    },

    // listStaffLeave fetches planned leave/absence records for operators.
    async listStaffLeave(
      params?: AdminApiPaths['/admin/roster/leave']['get']['parameters']['query']
    ) {
      const query = new URLSearchParams();
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          if (v !== undefined && v !== null) {
            query.append(k, String(v));
          }
        });
      }
      const url = `/api/admin/roster/leave${query.toString() ? '?' + query.toString() : ''}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch leave: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['StaffLeaveListResponse'];
      return { data: body };
    },

    // applyStaffLeave adds a new leave/absence period for an operator.
    async applyStaffLeave(
      requestBody: AdminApiComponents['schemas']['ApplyStaffLeaveRequest']
    ) {
      const response = await fetch('/api/admin/roster/leave', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        // Surface the backend's business message (e.g. the min-operator coverage
        // block) instead of a bare "Conflict", so the operator sees the reason.
        const errBody = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(errBody?.message ?? `Failed to apply leave: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['StaffLeaveResponse'];
      return { data: body };
    },

    async approveStaffLeave(
      absenceId: string,
      requestBody: AdminApiComponents['schemas']['ApproveStaffLeaveRequest'],
      idempotencyKey: string = crypto.randomUUID()
    ) {
      const response = await fetch(`/api/admin/roster/leave/${encodeURIComponent(absenceId)}/approve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
        body: JSON.stringify(requestBody),
      });
      if (!response.ok) {
        throw new Error(`Failed to approve leave: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['StaffLeaveResponse'];
      return { data: body };
    },
  };
}
