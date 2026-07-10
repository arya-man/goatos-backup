'use client';

import { getAdminApi } from '@/lib/api/client';
import { useEffect, useState } from 'react';

interface Shift {
  position_code: string;
  position_title: string;
  shift_time: string;
  cbe_holder: string | null;
  cpt_holder: string | null;
  week_off_day: string;
  backup_group: string;
  is_backup_position: boolean;
}

export function TimetablePanel() {
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      try {
        const api = getAdminApi();
        const response = await api.getAdminRosterPositions({});

        if (response.data) {
          // Transform positions data into shift roster format
          setShifts(response.data as Shift[]);
        }

        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load shifts');
        console.error('Error loading shifts:', err);
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
    return <div className="p-4 text-red-600">Error: {error}</div>;
  }

  return (
    <div className="subpanel" data-sub="timetable">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>Timetable</b></div>
          <h1>Timetable</h1>
          <div className="sub">
            The shift roster per center — the operational source for <b>who executes each day</b>. Vaccination and every drive read this to resolve the owner. The mobile app <b>mirrors this read-only</b>. Edited here (web CRUD) only.
          </div>
        </div>
        <div className="sp"></div>
        <button className="btn" title="Import sheet">
          <svg className="ic" style={{ width: '13px' }}><use href="#i-arrow"/></svg>Import sheet
        </button>
        <button className="btn p" title="Edit shift">
          <svg className="ic"><use href="#i-plus"/></svg>Edit shift
        </button>
      </div>

      <div className="card">
        <div className="hd">
          <svg className="ic" style={{ color: 'var(--brand)' }}><use href="#i-clock"/></svg>
          <h3>Shift roster — CBE + CPT</h3>
          <div className="sp"></div>
          <span className="small muted">position × center · Week OFF · Backup</span>
        </div>
        <div className="bd" style={{ padding: 0 }}>
          <table id="ttTbl">
            <thead>
              <tr>
                <th>Operational position</th>
                <th>Shift</th>
                <th>CBE</th>
                <th>CPT</th>
                <th>Week OFF</th>
                <th>Backup</th>
              </tr>
            </thead>
            <tbody>
              {shifts.map((shift) => (
                <tr
                  key={shift.position_code}
                  style={shift.is_backup_position ? { background: 'var(--surf3)' } : undefined}
                >
                  <td>
                    {shift.is_backup_position ? <b>{shift.position_title}</b> : shift.position_title}
                  </td>
                  <td>{shift.shift_time}</td>
                  <td>{shift.cbe_holder || '—'}</td>
                  <td>{shift.cpt_holder || '—'}</td>
                  <td>{shift.week_off_day}</td>
                  <td>
                    <span className={`tag ${shift.is_backup_position ? 't-teal' : 't-mut'}`}>
                      {shift.is_backup_position ? `covers ${shift.backup_group}` : shift.backup_group}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="note" style={{ margin: '12px 14px' }}>
            The <b>Backup</b> column is a fixed <b>position</b>, not a leave-time pick — exactly as the source roster encodes it. A position's <b>Week OFF</b> auto-triggers same-day coverage from its backup, no leave request needed. Mobile shows this table read-only plus a "you're covering X" banner when a coverage window is active.
          </div>
        </div>
      </div>
    </div>
  );
}
