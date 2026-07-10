// Client-side API client for making requests from 'use client' components.
// This is a thin wrapper around fetch that calls the backend admin API endpoints.

import type { AdminApiComponents, AdminApiPaths } from '@goatos/api-client';

interface ApiResponse<T> {
  data?: T;
  error?: string;
}

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
      const data = (await response.json()) as ApiResponse<
        AdminApiComponents['schemas']['PositionListResponse']
      >;
      return data;
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
      const data = (await response.json()) as ApiResponse<{
        items: AdminApiComponents['schemas']['BackupConfig'][];
        trace_id: string;
      }>;
      return data;
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
      const data = (await response.json()) as ApiResponse<{
        items: AdminApiComponents['schemas']['Coverage'][];
        trace_id: string;
      }>;
      return data;
    },
  };
}
