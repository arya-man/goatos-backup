'use client';

import { getAdminApi } from '@/lib/api/client';
import { type ParkScopeOption } from '@/lib/api/park-scope';
import { loadVaccinationOperatorsScreen } from './vaccination-operators-scope';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';
import type { AdminApiComponents } from '@goatos/api-client';
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';

type Position = AdminApiComponents['schemas']['Position'];
type StaffLeave = AdminApiComponents['schemas']['StaffLeaveListResponse']['items'][number];

interface VaccinationOperatorsScreenProps {
  pageContract?: AdminUiPageContract;
}

type VaccinationOperatorAssignmentConfig = import('@goatos/api-client').AppApiComponents['schemas']['VaccinationOperatorAssignmentConfig'];
type VaccinationOperatorShift = import('@goatos/api-client').AppApiComponents['schemas']['VaccinationOperatorShift'];

const WEEKDAYS = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday'] as const;
const WEEK_LABELS: Record<string, string> = {
  monday: 'Mon',
  tuesday: 'Tue',
  wednesday: 'Wed',
  thursday: 'Thu',
  friday: 'Fri',
  saturday: 'Sat',
  sunday: 'Sun',
};

const MON_NAMES = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DOW_NAMES = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

function todayISO(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function iso(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function pad(n: number): string {
  return String(n).padStart(2, '0');
}

function minutesToHHMM(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return `${pad(hours)}:${pad(mins)}`;
}

function nextDay(s: string): string {
  const d = new Date(s + 'T00:00:00');
  d.setDate(d.getDate() + 1);
  return iso(d);
}

function daysIn(r: { from: string; to: string }): number {
  const d = new Date(r.from + 'T00:00:00');
  const e = new Date(r.to + 'T00:00:00');
  return Math.round((e.getTime() - d.getTime()) / 86400000) + 1;
}

function fmtRange(r: { from: string; to: string }): string {
  const f = new Date(r.from + 'T00:00:00');
  const t = new Date(r.to + 'T00:00:00');
  const o: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' };
  return r.from === r.to ? f.toLocaleDateString('en-US', o) : `${f.toLocaleDateString('en-US', o)} – ${t.toLocaleDateString('en-US', o)}`;
}

// Merge overlapping/adjacent ranges
function mergeLeaves(leaves: { from: string; to: string }[]): { from: string; to: string }[] {
  const sorted = [...leaves].sort((a, b) => (a.from < b.from ? -1 : 1));
  const out: { from: string; to: string }[] = [];
  for (const r of sorted) {
    const last = out[out.length - 1];
    if (last && r.from <= nextDay(last.to)) {
      if (r.to > last.to) last.to = r.to;
    } else {
      out.push({ ...r });
    }
  }
  return out;
}

// Dates booked by OTHER operators
function blockedDates(opId: string, allLeaves: Record<string, { from: string; to: string }[]>): Set<string> {
  const s = new Set<string>();
  for (const [id, leaves] of Object.entries(allLeaves)) {
    if (id === opId) continue;
    for (const r of leaves) {
      const d = new Date(r.from + 'T00:00:00');
      const e = new Date(r.to + 'T00:00:00');
      while (d <= e) {
        s.add(iso(d));
        d.setDate(d.getDate() + 1);
      }
    }
  }
  return s;
}

// This operator's own dates
function ownDates(opId: string, allLeaves: Record<string, { from: string; to: string }[]>): Set<string> {
  const s = new Set<string>();
  const leaves = allLeaves[opId] ?? [];
  for (const r of leaves) {
    const d = new Date(r.from + 'T00:00:00');
    const e = new Date(r.to + 'T00:00:00');
    while (d <= e) {
      s.add(iso(d));
      d.setDate(d.getDate() + 1);
    }
  }
  return s;
}

export function VaccinationOperatorsScreen({}: VaccinationOperatorsScreenProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [commonCap, setCommonCap] = useState(200);
  const [operatorCount, setOperatorCount] = useState(1);
  const [defaultOperator, setDefaultOperator] = useState<string>('');
  const [assignmentConfig, setAssignmentConfig] = useState<VaccinationOperatorAssignmentConfig | null>(null);
  // Park scope as RESOLVED BY THE BACKEND (BUG-019) — never inferred from row data.
  const [parkId, setParkId] = useState<string | null>(null);
  // Backend-owned park vocabulary, present ONLY when the caller's scope covers several parks.
  // A park-scoped actor never sees these and never clicks anything (parkChoices stays null).
  const [parkChoices, setParkChoices] = useState<ParkScopeOption[] | null>(null);
  const [parkChoiceMessage, setParkChoiceMessage] = useState('');
  // The park the actor picked. There is deliberately NO local default: a pre-selected park would be
  // this screen inventing scope, which is the defect BUG-019 is about.
  const [chosenParkId, setChosenParkId] = useState<string | null>(null);
  const [parkDraft, setParkDraft] = useState('');
  const [rowVersion, setRowVersion] = useState(0);
  const [configSaving, setConfigSaving] = useState(false);

  const [leaves, setLeaves] = useState<Record<string, { from: string; to: string }[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Drawer state
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerTarget, setDrawerTarget] = useState<string | null>(null);

  // Modal state
  const [modalOpen, setModalOpen] = useState(false);
  const [modalTarget, setModalTarget] = useState<string | null>(null);
  const [viewYear, setViewYear] = useState(new Date().getFullYear());
  const [viewMonth, setViewMonth] = useState(new Date().getMonth());
  const [selFrom, setSelFrom] = useState<string | null>(null);
  const [selTo, setSelTo] = useState<string | null>(null);
  const [modalError, setModalError] = useState<string>('');

  const [toast, setToast] = useState<string>('');

  const showToast = useCallback((msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(''), 3400);
  }, []);

  // Load data
  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        // Re-entering the effect after a park is chosen must show the loading state again rather
        // than flashing the previous (unscoped) render. Set inside the async body, not the effect
        // body, so it is not a synchronous cascading render.
        setLoading(true);
        const api = getAdminApi();

        // BUG-019: park scope is BACKEND-owned. `loadVaccinationOperatorsScreen` asks the backend to
        // resolve the caller's scope (no park_id on the first call), then scopes EVERY downstream read
        // — roster, capacity KPI, weekly preview, default-operator dropdown — to exactly that park.
        // Deriving the park from the first row of an unscoped roster blended every park of a
        // multi-park tenant into one screen. When the caller's scope covers several parks the backend
        // returns the parks they may choose from, and this screen renders that selector rather than
        // dying — a tenant-wide (ceo_internal) actor must still be able to use the screen.
        const result = await loadVaccinationOperatorsScreen(api, chosenParkId ?? undefined);
        if (!alive) return;
        if (result.state === 'needs_park_selection') {
          setParkChoices(result.parks);
          setParkChoiceMessage(result.message);
          setParkId(null);
          setError(null);
          return;
        }
        setParkChoices(null);
        const resolvedParkId = result.parkId;
        const config = (result.config as VaccinationOperatorAssignmentConfig | null) ?? null;
        setParkId(resolvedParkId);
        if (config) {
          setAssignmentConfig(config);
          setRowVersion(config.rowVersion);
          setOperatorCount(config.activeOperatorsPerDay);
          // Store as workforce_member_id (from config), not position_id
          setDefaultOperator(config.defaultOperatorId);
        }

        const pos = (result.positions as Position[]) ?? [];
        setPositions(pos);
        setCommonCap(result.commonCap);
        if (!config) {
          // No config authored yet: fall back to the first NON-BACKUP operator of
          // THIS park. operatorsList/orderedOps filter out backup slots, so a
          // backup-slot default would highlight the wrong operator.
          const firstNonBackupOp = pos.find((p) => !p.is_backup_slot);
          if (firstNonBackupOp?.workforce_member_id) setDefaultOperator(firstNonBackupOp.workforce_member_id);
        }

        // Map leaves from backend by workforce_member_id
        const leavesMap: Record<string, { from: string; to: string }[]> = {};
        for (const p of pos) {
          leavesMap[p.position_id ?? ''] = [];
        }
        const leaveItems = (result.leaveItems as StaffLeave[]) ?? [];
        for (const leave of leaveItems) {
          // Only include approved or reported leaves (not rejected/canceled)
          if (leave.status === 'approved' || leave.status === 'reported') {
            const wfId = leave.workforce_member_id;
            // Find position by workforce_member_id
            const match = pos.find((p) => p.workforce_member_id === wfId);
            if (match?.position_id) {
              if (!leavesMap[match.position_id]) leavesMap[match.position_id] = [];
              // Convert timestamps to date strings (YYYY-MM-DD)
              const fromStr = leave.starts_at.split('T')[0];
              const toStr = leave.ends_at.split('T')[0];
              leavesMap[match.position_id].push({ from: fromStr, to: toStr });
            }
          }
        }
        setLeaves(leavesMap);
        setError(null);
      } catch (err) {
        if (alive) setError(err instanceof Error ? err.message : 'Failed to load');
      } finally {
        if (alive) setLoading(false);
      }
    };
    load();
    return () => {
      alive = false;
    };
  }, [chosenParkId]);

  // Drawer
  const openDrawer = (opId: string) => {
    setDrawerTarget(opId);
    setDrawerOpen(true);
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setDrawerTarget(null);
  };

  // Modal
  const openModal = (opId: string) => {
    setModalTarget(opId);
    setSelFrom(null);
    setSelTo(null);
    setModalError('');
    setViewYear(new Date().getFullYear());
    setViewMonth(new Date().getMonth());
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setModalTarget(null);
  };

  // Escape closes the open overlay (modal takes priority over drawer). Client-local only.
  useEffect(() => {
    if (!modalOpen && !drawerOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (modalOpen) {
        closeModal();
      } else if (drawerOpen) {
        closeDrawer();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [modalOpen, drawerOpen]);

  // Persist operator count and default operator to backend
  const persistOperatorConfig = async () => {
    if (!parkId || !assignmentConfig) {
      showToast('Configuration not ready. Please refresh.');
      return;
    }

    setConfigSaving(true);
    try {
      const api = getAdminApi();
      const result = await api.putVaccinationOperatorAssignmentConfig({
        parkId,
        activeOperatorsPerDay: operatorCount,
        defaultOperatorId: defaultOperator,
        rowVersion,
      });
      if (result.data) {
        setRowVersion(result.data.rowVersion);
        showToast(`<b style="color:var(--brand)">Saved</b> · ${operatorCount} operator${operatorCount !== 1 ? 's' : ''}/day, default set`);
      }
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : 'Failed to save';
      if (errMsg.includes('409') || errMsg.includes('conflict')) {
        // Row version conflict — refetch config
        showToast('Configuration changed elsewhere. Reloading...');
        try {
          const api = getAdminApi();
          const refreshResult = await api.getVaccinationOperatorAssignmentConfig(parkId);
          if (refreshResult.data) {
            setAssignmentConfig(refreshResult.data);
            setRowVersion(refreshResult.data.rowVersion);
            setOperatorCount(refreshResult.data.activeOperatorsPerDay);
            setDefaultOperator(refreshResult.data.defaultOperatorId);
          }
        } catch (reloadErr) {
          console.error('Failed to reload config:', reloadErr);
        }
      } else {
        showToast(`Error: ${errMsg}`);
      }
    } finally {
      setConfigSaving(false);
    }
  };

  // Add leave
  const addLeave = async () => {
    if (!selFrom || !modalTarget) {
      setModalError('Pick a start date on the calendar.');
      return;
    }
    const range = { from: selFrom, to: selTo || selFrom };

    // Check conflicts with other operators
    const blocked = blockedDates(modalTarget, leaves);
    const d = new Date(range.from + 'T00:00:00');
    const e = new Date(range.to + 'T00:00:00');
    while (d <= e) {
      if (blocked.has(iso(d))) {
        setModalError(`Range crosses a booked date. Pick a clear span.`);
        return;
      }
      d.setDate(d.getDate() + 1);
    }

    // Find the operator position to get workforce_member_id and scope
    const targetPos = positions.find((p) => p.position_id === modalTarget);
    if (!targetPos?.workforce_member_id) {
      setModalError('Operator position data missing');
      return;
    }

    // Commit to backend
    try {
      const api = getAdminApi();
      // Use position's scope for the leave request
      await api.applyStaffLeave({
        workforce_member_id: targetPos.workforce_member_id,
        scope_type: 'center', // Use center scope as default
        scope_id: targetPos.scope_id ?? '', // Use position's scope_id
        reason_code: 'planned_leave',
        starts_on: range.from,
        ends_on: range.to,
      });

      // Optimistically update local state and refetch
      const newLeaves = [...(leaves[modalTarget] ?? []), range];
      setLeaves({
        ...leaves,
        [modalTarget]: mergeLeaves(newLeaves),
      });
      closeModal();
      showToast('<b style="color:var(--brand)">Leave added</b>');
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to add leave');
    }
  };

  // Remove leave
  const removeLeave = (opId: string, fromDate: string) => {
    const updated = (leaves[opId] ?? []).filter((r) => r.from !== fromDate);
    setLeaves({
      ...leaves,
      [opId]: updated,
    });
    showToast('<b style="color:var(--brand)">Leave removed</b>');
  };

  const operatorsList = useMemo(() => positions.filter((p) => !p.is_backup_slot), [positions]);

  // Get shift for an operator by workforce_member_id
  const getShiftForOperator = (workforceMemberId: string): VaccinationOperatorShift | undefined => {
    return assignmentConfig?.shifts.find((s) => s.operatorId === workforceMemberId);
  };

  // Drive-operator ordering + weekly assignment preview — backend-config-driven
  // (N + default from config, PM fallback from config shifts, fallback chain ordered by shift)
  const weekOffOf = (op: Position): string => (op.week_off_weekday ?? op.week_off ?? '').toLowerCase();
  const firstName = (op?: Position): string => (op?.person_display_name ?? 'Operator').split(' ')[0];

  const orderedOps = useMemo(() => {
    // For N=1, default is from backend config (stored as workforce_member_id)
    // For N>1, just use the roster order
    if (operatorCount !== 1) {
      return [...operatorsList];
    }

    // Map defaultOperator (workforce_member_id) to Position
    const defaultPos = operatorsList.find((op) => op.workforce_member_id === defaultOperator);
    if (!defaultPos) return [...operatorsList];

    const rest = operatorsList.filter((op) => op.position_id !== defaultPos.position_id);
    return [defaultPos, ...rest];
  }, [operatorsList, operatorCount, defaultOperator]);

  const weeklyPlan = useMemo(() => {
    // Find PM operator for fallback chains
    let pmOp: Position | undefined;
    if (assignmentConfig?.shifts) {
      const pmShift = assignmentConfig.shifts.find((s) => s.shiftLabel === 'pm');
      if (pmShift) {
        pmOp = operatorsList.find((op) => op.workforce_member_id === pmShift.operatorId);
      }
    }

    // Map a recurring weekday to its next real occurrence (today or forward within 7 days),
    // so date-range leave applies to the preview the same way the backend resolves per date.
    const dateForDow = (dow: string): string => {
      const target = WEEKDAYS.indexOf(dow as (typeof WEEKDAYS)[number]);
      const now = new Date();
      const todayIdx = (now.getDay() + 6) % 7; // Monday=0
      const delta = (target - todayIdx + 7) % 7;
      const d = new Date(now);
      d.setDate(d.getDate() + delta);
      return iso(d);
    };
    const onLeaveForDate = (op: Position, dateStr: string): boolean =>
      (leaves[op.position_id ?? ''] ?? []).some((r) => dateStr >= r.from && dateStr <= r.to);

    return WEEKDAYS.map((dow) => {
      const dateStr = dateForDow(dow);
      const isWeekOff = (o: Position) => weekOffOf(o) === dow;
      const isOnLeave = (o: Position) => onLeaveForDate(o, dateStr);
      const avail = orderedOps.filter((o) => !isWeekOff(o) && !isOnLeave(o));
      const chosen = avail.slice(0, operatorCount);
      if (operatorCount === 1) {
        if (!chosen.length) return { dow, ops: [], reason: 'no operator available', kind: 'danger' as const };
        const op = chosen[0];
        const def = orderedOps[0];
        if (op.position_id === def?.position_id) return { dow, ops: chosen, reason: 'default', kind: 'brand' as const };
        const fallbackName = pmOp ? firstName(pmOp) : firstName(op);
        const defName = def ? firstName(def) : 'Default';
        // Distinguish WHY the default dropped out: leave vs recurring week-off.
        const defWhy = def && isOnLeave(def) ? 'on leave' : 'week-off';
        const kind = defWhy === 'on leave' ? ('warn' as const) : ('info' as const);
        return { dow, ops: chosen, reason: `${defName} ${defWhy} → ${fallbackName} covers`, kind };
      }
      const off = orderedOps.filter((o) => isWeekOff(o) || isOnLeave(o));
      return {
        dow,
        ops: chosen,
        reason: off.length
          ? `${chosen.length} on · ${off.map((o) => `${firstName(o)} ${isOnLeave(o) ? 'leave' : 'off'}`).join(', ')}`
          : `all ${operatorCount} parallel`,
        kind: off.length ? ('info' as const) : ('brand' as const),
      };
    });
  }, [orderedOps, operatorCount, assignmentConfig, operatorsList, leaves]);

  const kindColor: Record<string, string> = { brand: 'var(--brand)', info: 'var(--info)', warn: 'var(--warn)', danger: 'var(--danger)' };

  // KPIs
  const kpiOperators = operatorsList.length;
  const kpiDaily = operatorCount * commonCap;

  // Render calendar for modal
  const calendarDays: React.ReactNode[] = [];
  const dow = DOW_NAMES.map((d) => (
    <div key={d} className="cal-dow">
      {d}
    </div>
  ));

  const first = new Date(viewYear, viewMonth, 1);
  const start = first.getDay();
  const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
  const blocked = blockedDates(modalTarget || '', leaves);
  const own = ownDates(modalTarget || '', leaves);
  const today = todayISO();

  for (let i = 0; i < start; i++) {
    calendarDays.push(<div key={`pad-${i}`} className="cal-day muted"></div>);
  }

  for (let day = 1; day <= daysInMonth; day++) {
    const ds = `${viewYear}-${pad(viewMonth + 1)}-${pad(day)}`;
    let cls = 'cal-day';
    const isPast = ds < today;
    if (isPast) {
      cls += ' muted';
    } else if (blocked.has(ds)) {
      cls += ' dis';
    } else if (own.has(ds)) {
      cls += ' own';
    } else if (selFrom && selTo && ds >= selFrom && ds <= selTo) {
      cls += (ds === selFrom || ds === selTo) ? ' end' : ' inrange';
    } else if (selFrom && !selTo && ds === selFrom) {
      cls += ' end';
    }

    calendarDays.push(
      <div
        key={ds}
        className={cls}
        data-d={ds}
        onClick={() => {
          if (isPast || blocked.has(ds) || own.has(ds)) return;
          if (!selFrom || (selFrom && selTo)) {
            setSelFrom(ds);
            setSelTo(null);
          } else if (ds < selFrom) {
            setSelFrom(ds);
          } else {
            // Check range doesn't cross blocked/own
            let ok = true;
            const testD = new Date(selFrom + 'T00:00:00');
            const testE = new Date(ds + 'T00:00:00');
            while (testD <= testE) {
              if (blocked.has(iso(testD)) || own.has(iso(testD))) {
                ok = false;
                break;
              }
              testD.setDate(testD.getDate() + 1);
            }
            if (!ok) {
              setModalError('Range crosses a blocked or already-planned date. Pick a clear span.');
            } else {
              setSelTo(ds);
              setModalError('');
            }
          }
        }}
      >
        {day}
      </div>
    );
  }

  if (loading) return <div className="p-6">Loading...</div>;
  if (error) return <div className="p-6 text-red-600">{error}</div>;

  // BUG-019: a caller whose authorized scope covers several parks must CHOOSE one before any roster,
  // capacity KPI, weekly preview, or default-operator dropdown is rendered — those are all park-scoped
  // and blending them was the defect. The options, their labels, and the reason copy are backend-owned
  // (409 `park_scope_ambiguous`); this screen only renders them and sends the chosen parkId back.
  // Nothing is preselected, so no local default can be silently overwritten by a later response.
  if (parkChoices) {
    return (
      <section className="screen on" data-screen="vaccination-operators">
        <div className="phead">
          <div>
            <div className="crumb">Team / <b>Vaccination operators</b></div>
            <h1>Vaccination operators</h1>
            <div className="sub">{parkChoiceMessage}</div>
          </div>
        </div>
        <div className="card">
          <div className="hd">
            <h3>Choose a park</h3>
            <div className="sp"></div>
          </div>
          <div className="bd">
            <div className="ctl">
              <div className="fld">
                <label>Park</label>
                <select
                  value={parkDraft}
                  onChange={(e) => setParkDraft(e.target.value)}
                  aria-label="Park scope"
                >
                  <option value="">— select a park —</option>
                  {parkChoices.map((park) => (
                    <option key={park.parkId} value={park.parkId}>
                      {park.code ? `${park.code} · ${park.name}` : park.name}
                    </option>
                  ))}
                </select>
              </div>
              <button
                className="btn b sm"
                style={{ marginTop: '16px' }}
                onClick={() => setChosenParkId(parkDraft)}
                disabled={!parkDraft}
                aria-disabled={!parkDraft}
                title={!parkDraft ? 'Select a park to load its roster' : 'Load this park’s roster'}
              >
                Continue
              </button>
            </div>
            <div className="note" style={{ marginTop: '10px' }}>
              Roster, operator caps, the weekly assignment preview, and the default operator are all
              per-park. One park is loaded at a time so no two parks are ever mixed on this screen.
            </div>
            {parkChoices.length === 0 && (
              <div className="lvempty" style={{ marginTop: '12px' }}>
                No park is available for your access yet.
              </div>
            )}
          </div>
        </div>
      </section>
    );
  }

  const drawerOp = drawerTarget ? operatorsList.find((p) => p.position_id === drawerTarget) : null;
  const drawerLeaves = drawerTarget ? (leaves[drawerTarget] ?? []) : [];
  const drawerUpcoming = drawerLeaves.filter((r) => r.to >= today).sort((a, b) => (a.from < b.from ? -1 : 1));
  const drawerPast = drawerLeaves.filter((r) => r.to < today).sort((a, b) => (a.from < b.from ? 1 : -1));

  return (
    <section className="screen on" data-screen="vaccination-operators">
      <div className="phead">
        <div>
          <div className="crumb">Team / <b>Vaccination operators</b></div>
          <h1>Vaccination operators</h1>
          <div className="sub">CPT · Channapatna. Roster, weekly availability, and drive-operator assignment on one screen. Operator caps drive vaccination scheduling; week-off and leave remove an operator from that day.</div>
        </div>
      </div>

      {/* KPI Row */}
      <div className="grid g4" style={{ marginTop: '14px' }}>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--brand)' }}></span>
          <div className="lab">Operators</div>
          <div className="val">{kpiOperators}</div>
          <div className="dl">active vaccination seats</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--amber)' }}></span>
          <div className="lab">Cap / operator</div>
          <div className="val">{commonCap}</div>
          <div className="dl">applies to all operators</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--teal)' }}></span>
          <div className="lab">Operators / day</div>
          <div className="val">{operatorCount}</div>
          <div className="dl">{operatorCount === 1 ? 'single + fallback' : operatorCount === 3 ? 'all parallel' : 'pair'}</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: 'var(--info)' }}></span>
          <div className="lab">Daily capacity</div>
          <div className="val">{kpiDaily}</div>
          <div className="dl">at full availability</div>
        </div>
      </div>

      {/* Roster & Availability Card */}
      <div className="card">
        <div className="hd">
          <h3>Operator roster & availability</h3>
          <div className="sp"></div>
          {/* BUG-020: the cap edit form is NOT rendered at all until a real
              write endpoint exists. It previously stayed mounted behind a
              disabled Edit button and its Save handler reported a fabricated
              "Saved" toast with no backend call. */}
          <div className="capctl">
            <span className="capctl-lab">Cap / operator</span>
            <b id="capText">{commonCap}</b>
            <span className="capunit">animals/day</span>
            <button
              className="btn sm"
              aria-disabled={true}
              title="Common-cap write not yet available (backend pending)"
              disabled
            >
              ✏️ Edit
            </button>
          </div>
        </div>
        <div className="bd" style={{ overflowX: 'auto' }}>
          <table>
            <thead>
              <tr>
                <th>Person</th>
                <th>Park</th>
                <th>Shift</th>
                <th>Week off</th>
                <th>Planned leave</th>
                <th>Weekly schedule</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {operatorsList.map((op) => {
                const opLeaves = leaves[op.position_id ?? ''] ?? [];
                const opUpcoming = opLeaves.filter((r) => r.to >= today).sort((a, b) => (a.from < b.from ? -1 : 1));
                const nextRange = opUpcoming[0];
                const init = (op.person_display_name ?? 'OP')[0];
                const shortName = op.person_display_name ?? 'Operator';
                const weekOff = op.week_off_weekday ?? op.week_off ?? '—';
                const weekOffLabel = WEEKDAYS.includes(weekOff as (typeof WEEKDAYS)[number]) ? WEEK_LABELS[weekOff as (typeof WEEKDAYS)[number]] : weekOff;
                const statusToday = new Date();
                const todayDow = WEEKDAYS[statusToday.getDay() === 0 ? 6 : statusToday.getDay() - 1];
                const onLeaveToday = opLeaves.some((r) => today >= r.from && today <= r.to);
                const weekOffToday = weekOff.toLowerCase() === todayDow;

                return (
                  <tr key={op.position_id}>
                    <td>
                      <div className="person">
                        <div className="av">{init}</div>
                        <div>
                          <b>{shortName}</b>
                          <span>Vaccination operator</span>
                        </div>
                      </div>
                    </td>
                    <td>Channapatna</td>
                    <td>
                      {(() => {
                        const shift = getShiftForOperator(op.workforce_member_id ?? '');
                        if (!shift) return '—';
                        const startTime = minutesToHHMM(shift.shiftStartMinute);
                        const endTime = minutesToHHMM(shift.shiftEndMinute);
                        const label = shift.shiftLabel.charAt(0).toUpperCase() + shift.shiftLabel.slice(1);
                        return `${label} · ${startTime}–${endTime}`;
                      })()}
                    </td>
                    <td>
                      <span className="tag t-info">{weekOffLabel}</span>
                    </td>
                    <td>
                      {!opLeaves.length ? (
                        <div className="leavecell">
                          <button
                            className="laddbtn"
                            onClick={() => openModal(op.position_id!)}
                          >
                            ＋ Add leave
                          </button>
                        </div>
                      ) : (
                        <div className="leavecell">
                          {nextRange ? (
                            <div
                              className="lsum"
                              onClick={() => openDrawer(op.position_id!)}
                              style={{ cursor: 'pointer' }}
                            >
                              <b>{fmtRange(nextRange)}</b>
                              <small>
                                {opLeaves.length} planned{opUpcoming.length > 1 ? ` · ${opUpcoming.length - 1} more upcoming` : ''}
                              </small>
                            </div>
                          ) : (
                            <div
                              className="lsum"
                              onClick={() => openDrawer(op.position_id!)}
                              style={{ cursor: 'pointer' }}
                            >
                              <b>none upcoming</b>
                              <small>{opLeaves.length} past</small>
                            </div>
                          )}
                          <button className="laddbtn" onClick={() => openModal(op.position_id!)}>
                            Manage
                          </button>
                        </div>
                      )}
                    </td>
                    <td>
                      <div className="wk">
                        {WEEKDAYS.map((dow) => {
                          const isOff = weekOff.toLowerCase() === dow;
                          return (
                            <div key={dow} className={`wc ${isOff ? 'off' : 'on'}`}>
                              <span>{WEEK_LABELS[dow]}</span>
                              <b>{isOff ? 'Off' : 'On'}</b>
                            </div>
                          );
                        })}
                      </div>
                    </td>
                    <td>
                      {onLeaveToday ? (
                        <span className="tag t-warn">On leave today</span>
                      ) : weekOffToday ? (
                        <span className="tag t-info">Week-off today</span>
                      ) : (
                        <span className="tag t-ok">Available</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* Drive Operator Assignment */}
      <div className="card">
        <div className="hd">
          <h3>Drive operator assignment</h3>
          <div className="sp"></div>
        </div>
        <div className="bd">
          <div className="ctl">
            <div className="fld">
              <label>Active operators / day</label>
              <select
                value={operatorCount}
                onChange={(e) => setOperatorCount(parseInt(e.target.value, 10))}
                disabled={configSaving}
                title="Drives the live preview below."
              >
                <option value="1">1 operator</option>
                <option value="2">2 operators</option>
                <option value="3">3 operators</option>
              </select>
            </div>
            <div className="fld">
              <label>Default operator</label>
              <select
                value={defaultOperator}
                onChange={(e) => setDefaultOperator(e.target.value)}
                disabled={operatorCount !== 1 || configSaving}
                aria-disabled={operatorCount !== 1 || configSaving}
                title={operatorCount !== 1 ? 'Only available when active operators = 1' : 'CEO default. Drives the live preview.'}
              >
                {operatorsList.map((op) => (
                  <option key={op.position_id} value={op.workforce_member_id ?? op.position_id}>
                    {op.person_display_name || 'Operator'}
                  </option>
                ))}
              </select>
            </div>
            <button
              className="btn b sm"
              onClick={persistOperatorConfig}
              disabled={configSaving}
              style={{ marginTop: '16px' }}
            >
              Save configuration
            </button>
          </div>
          <div className="note" style={{ marginTop: '10px' }}>
            Live preview of the assignment logic from the roster + week-offs. Configure and save active-operators
            and default operator settings to the backend.
          </div>
          <div className="uline" style={{ marginTop: '14px' }}>
            {operatorCount === 1 ? 'Fallback chain — first available wins' : `Parallel — up to ${operatorCount}/day run together`}
          </div>
          <div className="chain" style={{ marginTop: '10px' }}>
            {orderedOps.map((op, i) => (
              <Fragment key={op.position_id}>
                {i > 0 && <span className="carrow">{operatorCount === 1 ? '→' : '+'}</span>}
                <div className={`cnode${operatorCount === 1 && i === 0 ? ' default' : ''}${i >= operatorCount ? ' down' : ''}`}>
                  <div className="av">{(op.person_display_name ?? 'OP')[0]}</div>
                  <div>
                    <b>
                      {firstName(op)} {operatorCount === 1 && i === 0 && <span className="badge-def">DEFAULT</span>}
                    </b>
                    <small>off: {WEEK_LABELS[weekOffOf(op)] ?? '—'}</small>
                  </div>
                </div>
              </Fragment>
            ))}
          </div>
          <div className="banner" style={{ marginTop: '10px', display: operatorCount !== 1 ? 'block' : 'none' }}>
            <b>Parallel mode:</b> up to {operatorCount} available operators run together. No single default; week-off/leave just drops that operator&apos;s slice for the day.
          </div>
          <div className="uline" style={{ marginTop: '20px' }}>
            Weekly assignment preview — who runs the drive each day
          </div>
          <div style={{ overflowX: 'auto', marginTop: '12px' }}>
            <table>
              <thead>
                <tr>
                  <th style={{ width: '80px' }}>Day</th>
                  <th>Assigned operator</th>
                  <th>Reason</th>
                </tr>
              </thead>
              <tbody>
                {weeklyPlan.map((p) => (
                  <tr key={p.dow}>
                    <td><b>{WEEK_LABELS[p.dow]}</b></td>
                    <td>
                      {p.ops.length ? (
                        p.ops.map((o) => (
                          <span key={o.position_id} className="op" style={{ marginRight: '14px' }}>
                            <span className="dot" style={{ background: kindColor[p.kind] }}></span>
                            {firstName(o)}
                          </span>
                        ))
                      ) : (
                        <span className="tag t-danger">— none —</span>
                      )}
                    </td>
                    <td className="why">{p.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* Right Drawer - Leave Details */}
      {drawerOpen && drawerOp && (
        <>
          <div
            className="dscrim on"
            onClick={closeDrawer}
          ></div>
          <aside className="drawer" role="dialog" aria-modal="true">
            <div className="dh">
              <div className="av">{(drawerOp.person_display_name ?? 'OP')[0]}</div>
              <div style={{ flex: 1 }}>
                <h3>{drawerOp.person_display_name || 'Operator'}</h3>
                <span>Planned leave</span>
              </div>
              <button className="cal-nav" onClick={closeDrawer} title="Close">
                ✕
              </button>
            </div>
            <div className="db">
              {!drawerLeaves.length ? (
                <div className="lvempty">No planned leave. Use &quot;Add leave&quot;.</div>
              ) : (
                <>
                  {drawerUpcoming.length > 0 && (
                    <>
                      <div className="dgrp">Upcoming</div>
                      {drawerUpcoming.map((r) => (
                        <div key={r.from} className="lvitem">
                          <div>
                            <div className="lvdate">{fmtRange(r)}</div>
                            <div className="lvdays">{daysIn(r)} day{daysIn(r) > 1 ? 's' : ''}</div>
                          </div>
                          <button
                            className="rm"
                            onClick={() => removeLeave(drawerTarget!, r.from)}
                            title="Leave cancellation not yet available"
                            disabled
                            aria-disabled={true}
                          >
                            ×
                          </button>
                        </div>
                      ))}
                    </>
                  )}
                  {drawerPast.length > 0 && (
                    <>
                      <div className="dgrp">Past</div>
                      {drawerPast.map((r) => (
                        <div key={r.from} className="lvitem past">
                          <div>
                            <div className="lvdate">{fmtRange(r)}</div>
                            <div className="lvdays">{daysIn(r)} day{daysIn(r) > 1 ? 's' : ''}</div>
                          </div>
                          <button
                            className="rm"
                            onClick={() => removeLeave(drawerTarget!, r.from)}
                            title="Leave cancellation not yet available"
                            disabled
                            aria-disabled={true}
                          >
                            ×
                          </button>
                        </div>
                      ))}
                    </>
                  )}
                </>
              )}
            </div>
            <div className="df">
              <button className="btn b" style={{ width: '100%' }} onClick={() => openModal(drawerTarget!)}>
                ＋ Add leave
              </button>
            </div>
          </aside>
        </>
      )}

      {/* Modal - Add Leave */}
      {modalOpen && (
        <>
          <div className="scrim on" onClick={closeModal}></div>
          <div className="modal" role="dialog" aria-modal="true">
            <div className="mh">
              <div className="av">{(operatorsList.find((p) => p.position_id === modalTarget)?.person_display_name ?? 'OP')[0]}</div>
              <div>
                <h3>Add planned leave</h3>
                <span>{operatorsList.find((p) => p.position_id === modalTarget)?.person_display_name || 'Operator'} · Vaccination operator</span>
              </div>
              <div style={{ flex: 1 }}></div>
              <button className="cal-nav" onClick={closeModal} title="Close">
                ✕
              </button>
            </div>
            <div className="mb">
              <div className="rangelab">
                <span>Pick leave dates</span>
                <b id="lmRange">
                  {!selFrom ? '— pick a start day —' : !selTo ? `${fmtRange({ from: selFrom, to: selFrom })} → pick end` : fmtRange({ from: selFrom, to: selTo })}
                </b>
              </div>
              <div className="cal">
                <div className="cal-h">
                  <button className="cal-nav" onClick={() => {
                    setViewMonth(v => v === 0 ? 11 : v - 1);
                    if (viewMonth === 0) setViewYear(y => y - 1);
                  }}>
                    ‹
                  </button>
                  <div className="mlab">{MON_NAMES[viewMonth]} {viewYear}</div>
                  <button className="cal-nav" onClick={() => {
                    setViewMonth(v => v === 11 ? 0 : v + 1);
                    if (viewMonth === 11) setViewYear(y => y + 1);
                  }}>
                    ›
                  </button>
                </div>
                <div className="cal-grid">{dow}</div>
                <div className="cal-grid">{calendarDays}</div>
              </div>
              <div className="legendcal">
                <span>
                  <i style={{ background: 'var(--brand)' }}></i>Selected
                </span>
                <span>
                  <i style={{ background: 'var(--warnx)', border: '1px solid var(--warn)' }}></i>Already planned
                </span>
                <span>
                  <i style={{ background: 'var(--dangerx)', border: '1px solid var(--danger)' }}></i>Booked by another
                </span>
              </div>
              {modalError && (
                <div className="err on" style={{ marginTop: '12px' }}>
                  {modalError}
                </div>
              )}
            </div>
            <div className="mf">
              <button className="btn sm ghost" onClick={closeModal}>
                Cancel
              </button>
              <button className="btn b sm" onClick={addLeave}>
                Add leave
              </button>
            </div>
          </div>
        </>
      )}

      {/* Toast */}
      {toast && (
        <div id="toast" className="show">
          <div dangerouslySetInnerHTML={{ __html: toast }} />
        </div>
      )}
    </section>
  );
}
