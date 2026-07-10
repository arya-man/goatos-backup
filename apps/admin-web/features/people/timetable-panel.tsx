'use client';

import { getAdminApi } from '@/lib/api/client';
import { useEffect, useState } from 'react';
import type { AdminApiComponents } from '@goatos/api-client';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';

// Backend enriches positions with display fields from related tables
type BasePosition = AdminApiComponents['schemas']['Position'];
interface Position extends BasePosition {
  person_display_name?: string | null;
  hr_designation_grade?: string | null;
  position_title?: string | null;
  center_label?: string | null;
  tier?: string | null;
  week_off?: string | null;
  backup_group?: string | null;
}

interface TimetablePanelProps {
  pageContract?: AdminUiPageContract;
}

export function TimetablePanel({ pageContract }: TimetablePanelProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      try {
        const api = getAdminApi();
        const response = await api.listStaffPositions();

        if (response.data?.items) {
          setPositions(response.data.items);
        }

        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load timetable');
        console.error('Error loading timetable:', err);
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, []);

  if (loading) {
    return <div className="p-4 text-center">Loading timetable...</div>;
  }

  if (error) {
    return (
      <div style={{ padding: '16px', color: 'var(--danger)' }}>
        <svg className="ic" style={{ marginRight: '8px' }}>
          <use href="#i-warn" />
        </svg>
        Error: {error}
      </div>
    );
  }

  // Group positions by center + backup status for timetable display
  const regularPositions = positions.filter(p => !p.is_backup_slot && p.status === 'active');
  const backupPositions = positions.filter(p => p.is_backup_slot && p.status === 'active');

  return (
    <div className="subpanel" data-sub="timetable">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>Timetable</b></div>
          <h1>Timetable</h1>
          <div className="sub">
            The shift roster — the operational source for <b>who executes each day</b>. Vaccination and operations read this to resolve responsibility. The mobile app <b>mirrors this read-only</b>. CRUD edit only here.
          </div>
        </div>
        <div className="sp"></div>
        <button className="btn" title="Import sheet" disabled aria-label="Import sheet (backend import contract pending)">
          <svg className="ic" style={{ width: '13px' }}>
            <use href="#i-arrow" />
          </svg>
          Import sheet
        </button>
        <button className="btn p" title="Edit position" disabled aria-label="Edit position (backend contract pending)">
          <svg className="ic">
            <use href="#i-plus" />
          </svg>
          Edit position
        </button>
      </div>

      <div className="card">
        <div className="hd">
          <svg className="ic" style={{ color: 'var(--brand)' }}>
            <use href="#i-clock" />
          </svg>
          <h3>Position roster — regular + backup</h3>
          <div className="sp"></div>
          <span className="small muted">position · tier · center · week OFF · backup</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
          <table id="ttTbl">
            <thead>
              <tr>
                <th>Position title</th>
                <th>Position code</th>
                <th>Tier</th>
                <th>Center</th>
                <th>Person</th>
                <th>Week OFF</th>
                <th>Backup group</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {positions.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '16px', color: 'var(--muted)' }}>
                    No positions in roster
                  </td>
                </tr>
              ) : (
                positions.map((pos) => (
                  <tr
                    key={pos.position_id}
                    style={pos.is_backup_slot ? { background: 'var(--surf3)' } : undefined}
                  >
                    <td>
                      {pos.is_backup_slot ? <b>{pos.position_title || pos.position_code}</b> : pos.position_title || pos.position_code}
                    </td>
                    <td><code>{pos.position_code}</code></td>
                    <td><span className="tag t-info">{pos.position_tier}</span></td>
                    <td>{pos.center_label || '—'}</td>
                    <td>{pos.person_display_name || '—'}</td>
                    <td>{pos.week_off || (pos.week_off_weekday ? pos.week_off_weekday : '—')}</td>
                    <td>
                      <span
                        className={`tag ${pos.is_backup_slot ? 't-teal' : 't-mut'}`}
                      >
                        {pos.is_backup_slot ? `backup: ${pos.backup_group_code}` : pos.backup_group_code || '—'}
                      </span>
                    </td>
                    <td>
                      <span className={`tag ${pos.status === 'active' ? 't-ok' : 't-warn'}`}>
                        {pos.status}
                      </span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            <b>Backup column:</b> The backup group code (e.g., "backup_manager", "backup_am1") is a fixed <b>position assignment</b>, not a temporary leave pick — exactly as stored. A position's <b>Week OFF</b> auto-triggers same-day coverage from its backup group, no leave request needed. Mobile shows this table read-only plus "covering X" context when a coverage is active.
          </div>
        </div>
      </div>
    </div>
  );
}
