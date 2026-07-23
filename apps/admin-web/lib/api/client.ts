// Client-side API client for making requests from 'use client' components.
// This is a thin wrapper around fetch that calls the backend admin API endpoints.

import type { AdminApiComponents, AdminApiPaths, AppApiComponents } from '@goatos/api-client';

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

    async getVaccinationOperatorAssignmentConfig(parkId: string) {
      const query = new URLSearchParams({ park_id: parkId });
      const response = await fetch(`/api/vaccination/operator-assignment/config?${query.toString()}`, { cache: 'no-store' });
      if (!response.ok) {
        throw new Error(`Failed to fetch vaccination operator assignment config: ${response.statusText}`);
      }
      const body = (await response.json()) as AppApiComponents['schemas']['VaccinationOperatorAssignmentConfig'];
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
        throw new Error(`Failed to apply leave: ${response.statusText}`);
      }
      const body = (await response.json()) as AdminApiComponents['schemas']['StaffLeaveResponse'];
      return { data: body };
    },
  };
}
