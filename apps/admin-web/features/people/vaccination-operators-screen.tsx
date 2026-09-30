'use client';
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import ButtonBase from "@mui/material/ButtonBase";
import Snackbar from "@mui/material/Snackbar";
import ToggleButton from "@mui/material/ToggleButton";
import dayjs from "dayjs";
import { varAlpha } from "minimal-shared/utils";
import { DateCalendar } from "@mui/x-date-pickers/DateCalendar";
import { PickerDay, type PickerDayProps } from "@mui/x-date-pickers/PickerDay";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom, type TableHeadCellProps } from "@/components/app/table/table-head-custom";

import { getAdminApi } from '@/lib/api/client';
import MenuItem from "@mui/material/MenuItem";
import { type ParkScopeOption } from '@/lib/api/park-scope';
import { loadVaccinationOperatorsScreen } from './vaccination-operators-scope';
import {
  SHIFT_LABEL_OPTIONS,
  WEEK_OFF_OPTIONS,
  draftFromShift,
  shiftRequestFromDraft,
  shiftSummary,
  type ShiftDraft,
} from './vaccination-operator-shift-form';
import { type AdminUiPageContract } from '@/lib/admin-ui-contract';
import type { AdminApiComponents } from '@goatos/api-client';
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import Typography from "@mui/material/Typography";
import MuiTextField from "@mui/material/TextField";
import { Caption } from "@/components/app/caption";
import { VaccinationDeskSkeleton, vaccinationDeskOpensOnChooser } from "./people-skeletons";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget } from "@/components/app/kpi-widget";
import { Avatar } from "@/components/app/avatar";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import CardContent from "@mui/material/CardContent";
import CardHeader from "@mui/material/CardHeader";
import Card from "@mui/material/Card";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/components/minimal/iconify";
import { MinimalDrawer } from "@/components/app/drawer";
import { useBackCloses } from "@/components/use-back-closes";

type Position = AdminApiComponents['schemas']['Position'];
type StaffLeave = AdminApiComponents['schemas']['StaffLeaveListResponse']['items'][number];

interface VaccinationOperatorsScreenProps {
  initialParkId?: string;
  pageContract?: AdminUiPageContract;
  /** The tenant's parks, from the backend bootstrap, so the screen can name the park it shows. */
  parks?: ParkScopeOption[];
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

// Roster table: the template table card (TableHeadCustom in a Scrollbar). The weekly-schedule
// column needs its seven day cells, so the table keeps a floor and scrolls inside its card
// instead of clipping at the card edge (J1 P0-3).
const ROSTER_HEAD: TableHeadCellProps[] = [
  { id: 'person', label: 'Person' },
  { id: 'park', label: 'Park' },
  { id: 'shift', label: 'Shift' },
  { id: 'cap', label: 'Cap' },
  { id: 'week_off', label: 'Week off' },
  { id: 'leave', label: 'Planned leave' },
  { id: 'schedule', label: 'Weekly schedule' },
  { id: 'status', label: 'Status' },
];
const ROSTER_MIN_WIDTH = 1280;
const WEEKLY_HEAD: TableHeadCellProps[] = [
  { id: 'day', label: 'Day', width: 80 },
  { id: 'operator', label: 'Assigned operator' },
  { id: 'reason', label: 'Reason' },
];
/** Weekly-preview reason kind -> palette key of its dot. */
const KIND_COLOR = { brand: 'primary', info: 'info', warn: 'warning', danger: 'error' } as const;
const LEAVE_LEGEND = [
  { label: 'Selected', color: 'primary', solid: true },
  { label: 'Already planned', color: 'warning', solid: false },
  { label: 'Booked by another', color: 'error', solid: false },
] as const;
const TOAST_MS = 3400;

type Toast = { title?: string; text: string; severity: 'success' | 'info' | 'warning' | 'error' };

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
  const o: Intl.DateTimeFormatOptions = { day: '2-digit', month: '2-digit', year: 'numeric' };
  return r.from === r.to ? f.toLocaleDateString('en-GB', o) : `${f.toLocaleDateString('en-GB', o)} – ${t.toLocaleDateString('en-GB', o)}`;
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

export function VaccinationOperatorsScreen({ initialParkId, parks = [] }: VaccinationOperatorsScreenProps) {
  const [positions, setPositions] = useState<Position[]>([]);
  const [commonCap, setCommonCap] = useState(200);
  const [operatorCount, setOperatorCount] = useState(1);
  const [defaultOperator, setDefaultOperator] = useState<string>('');
  const [selectedOperatorIds, setSelectedOperatorIds] = useState<string[]>([]);
  const [, setAssignmentConfig] = useState<VaccinationOperatorAssignmentConfig | null>(null);
  // Park scope as RESOLVED BY THE BACKEND (BUG-019) — never inferred from row data.
  const [parkId, setParkId] = useState<string | null>(null);
  // Backend-owned park vocabulary, present ONLY when the caller's scope covers several parks.
  // A park-scoped actor never sees these and never clicks anything (parkChoices stays null).
  const [parkChoices, setParkChoices] = useState<ParkScopeOption[] | null>(null);
  const [parkChoiceMessage, setParkChoiceMessage] = useState('');
  // The same backend-owned park list, KEPT after a park is chosen: the loaded screen offers it as a
  // Park switch so a multi-park actor can move between parks without leaving the page. Null for a
  // park-scoped actor, who has nothing to switch to.
  const [parkOptions, setParkOptions] = useState<ParkScopeOption[] | null>(null);
  // The park the actor picked. There is deliberately NO local default: a pre-selected park would be
  // this screen inventing scope, which is the defect BUG-019 is about.
  const [chosenParkId, setChosenParkId] = useState<string | null>(null);
  const [parkDraft, setParkDraft] = useState('');
  const [rowVersion, setRowVersion] = useState(0);
  const [configSaving, setConfigSaving] = useState(false);
  const [draftCaps, setDraftCaps] = useState<Record<string, string>>({});
  const [savingCap, setSavingCap] = useState<string | null>(null);
  const [capError, setCapError] = useState<string | null>(null);
  // Common operator cap + per-animal shot cap (tenant capacity config). Editing these writes to the
  // scheduler-read tables and emits vaccination.capacity.changed per park, re-planning all future drives.
  const [animalShotCap, setAnimalShotCap] = useState<number | null>(null);
  const [capRowVersion, setCapRowVersion] = useState(0);
  const [capConfigError, setCapConfigError] = useState<string | null>(null);
  const [capEditing, setCapEditing] = useState(false);
  const [draftAnimalCap, setDraftAnimalCap] = useState('');
  const [savingCapCfg, setSavingCapCfg] = useState(false);

  const [leaves, setLeaves] = useState<Record<string, { from: string; to: string }[]>>({});
  // Every shift authored for this park, read on its own so a park with no drive-operator
  // assignment yet (every newly added park) can still set up its operators' shifts.
  const [shifts, setShifts] = useState<VaccinationOperatorShift[]>([]);
  // Set / Edit shift form (one operator at a time, keyed by position id).
  const [shiftTarget, setShiftTarget] = useState<string | null>(null);
  const [shiftDraft, setShiftDraft] = useState<ShiftDraft>({ shiftLabel: '', shiftStart: '', shiftEnd: '', weekOffWeekday: '' });
  const [shiftError, setShiftError] = useState('');
  const [shiftSaving, setShiftSaving] = useState(false);
  // Clearing asks once, in place: the Clear button turns into Keep / Clear shift.
  const [clearArmed, setClearArmed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Drawer state
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerTarget, setDrawerTarget] = useState<string | null>(null);

  // Modal state
  const [modalOpen, setModalOpen] = useState(false);
  const [modalTarget, setModalTarget] = useState<string | null>(null);
  const [selFrom, setSelFrom] = useState<string | null>(null);
  const [selTo, setSelTo] = useState<string | null>(null);
  const [modalError, setModalError] = useState<string>('');

  const [toast, setToast] = useState<Toast | null>(null);
  const [toastOpen, setToastOpen] = useState(false);

  const showToast = useCallback((next: Toast) => {
    setToast(next);
    setToastOpen(true);
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
        const result = await loadVaccinationOperatorsScreen(api, chosenParkId ?? initialParkId);
        if (!alive) return;
        if (result.state === 'needs_park_selection') {
          setParkChoices(result.parks);
          setParkOptions(result.parks);
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
          setSelectedOperatorIds(config.selectedOperatorIds ?? []);
        }

        const pos = (result.positions as Position[]) ?? [];
        setPositions(pos);
        setCommonCap(result.commonCap);
        setAnimalShotCap(result.animalShotCap);
        setCapRowVersion(result.capRowVersion);
        setCapConfigError(result.capConfigError);
        if (!config) {
          const nonBackupOps = pos.filter((p) => !p.is_backup_slot);
          const firstNonBackupOp = nonBackupOps[0];
          const firstNonBackupId = firstNonBackupOp?.workforce_member_id ?? '';

          setAssignmentConfig(null);
          setRowVersion(0);
          setOperatorCount(Math.max(1, Math.min(3, nonBackupOps.length)));
          // No config authored yet: fall back to the first NON-BACKUP operator of
          // THIS park. operatorsList/orderedOps filter out backup slots, so a
          // backup-slot default would highlight the wrong operator.
          setDefaultOperator(firstNonBackupId);
          setSelectedOperatorIds(firstNonBackupId ? [firstNonBackupId] : []);
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
        setShifts((result.shifts as VaccinationOperatorShift[]) ?? []);
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
  }, [chosenParkId, initialParkId]);

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
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setModalTarget(null);
  };

  // Android Back closes the leave drawer / the add-leave dialog (house drawer rule). They share
  // one history entry: opening the dialog from the drawer keeps `open` true, so Back closes the
  // topmost (the dialog), and the drawer after it only via X / Escape / scrim.
  useBackCloses(drawerOpen || modalOpen, modalOpen ? closeModal : closeDrawer);

  // Shift form
  const openShiftForm = (op: Position) => {
    if (!op.position_id) return;
    const existing = getShiftForOperator(op.workforce_member_id ?? '');
    setShiftTarget(op.position_id);
    setShiftDraft(draftFromShift(existing, op.week_off_weekday ?? op.week_off ?? null));
    setShiftError('');
    setClearArmed(false);
  };

  const closeShiftForm = () => {
    if (shiftSaving) return;
    setShiftTarget(null);
    setShiftError('');
    setClearArmed(false);
  };

  const reloadShifts = async (forParkId: string) => {
    const api = getAdminApi();
    const refreshed = await api.listVaccinationOperatorShifts(forParkId);
    setShifts((refreshed.data?.shifts as VaccinationOperatorShift[]) ?? []);
  };

  const saveShift = async () => {
    const op = positions.find((p) => p.position_id === shiftTarget);
    if (!parkId || !op?.workforce_member_id) {
      setShiftError('This operator is not linked to a person yet, so a shift cannot be set.');
      return;
    }
    setShiftSaving(true);
    setShiftError('');
    try {
      const api = getAdminApi();
      await api.putVaccinationOperatorShift(shiftRequestFromDraft(parkId, op.workforce_member_id, shiftDraft));
      // serial-await: allow the reload must read the shift this write just saved
      await reloadShifts(parkId);
      setShiftTarget(null);
      showToast({ title: 'Shift saved', text: `${op.person_display_name ?? 'Operator'} — future vaccination drives are being re-planned`, severity: 'success' });
    } catch (err) {
      setShiftError(err instanceof Error ? err.message : 'The shift could not be saved. Try again.');
    } finally {
      setShiftSaving(false);
    }
  };

  const clearShift = async () => {
    const op = positions.find((p) => p.position_id === shiftTarget);
    if (!parkId || !op?.workforce_member_id) return;
    setShiftSaving(true);
    setShiftError('');
    try {
      const api = getAdminApi();
      await api.deleteVaccinationOperatorShift(parkId, op.workforce_member_id);
      // serial-await: allow the reload must read the shift this write just saved
      await reloadShifts(parkId);
      setShiftTarget(null);
      setClearArmed(false);
      showToast({ title: 'Shift cleared', text: op.person_display_name ?? 'Operator', severity: 'success' });
    } catch (err) {
      setClearArmed(false);
      setShiftError(err instanceof Error ? err.message : 'The shift could not be cleared. Try again.');
    } finally {
      setShiftSaving(false);
    }
  };

  // Escape closes the open overlay (modal takes priority over drawer). Client-local only.
  useEffect(() => {
    if (!modalOpen && !drawerOpen && !shiftTarget) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (shiftTarget) {
        if (!shiftSaving) {
          setShiftTarget(null);
          setShiftError('');
          setClearArmed(false);
        }
      } else if (modalOpen) {
        closeModal();
      } else if (drawerOpen) {
        closeDrawer();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [modalOpen, drawerOpen, shiftTarget, shiftSaving]);

  // Persist operator count and default operator to backend
  const persistOperatorConfig = async () => {
    if (!parkId || !defaultOperator) {
      showToast({ text: 'Configuration not ready. Please refresh.', severity: 'warning' });
      return;
    }

    setConfigSaving(true);
    try {
      const api = getAdminApi();
      const nextSelected = normalizeSelectedOperators(
        operatorCount === 1 ? [defaultOperator] : selectedOperatorIds,
        operatorCount,
        defaultOperator
      );
      const result = await api.putVaccinationOperatorAssignmentConfig({
        parkId,
        activeOperatorsPerDay: operatorCount,
        defaultOperatorId: defaultOperator,
        selectedOperatorIds: nextSelected,
        rowVersion,
      });
      if (result.data) {
        setAssignmentConfig(result.data);
        setRowVersion(result.data.rowVersion);
        setSelectedOperatorIds(result.data.selectedOperatorIds ?? nextSelected);
        showToast({ title: 'Saved', text: `${operatorCount} operator${operatorCount !== 1 ? 's' : ''}/day assigned`, severity: 'success' });
      }
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : 'Failed to save';
      if (errMsg.includes('409') || errMsg.includes('conflict')) {
        // Row version conflict — refetch config
        showToast({ text: 'Configuration changed elsewhere. Reloading...', severity: 'info' });
        try {
          const api = getAdminApi();
          const refreshResult = await api.getVaccinationOperatorAssignmentConfig(parkId);
          if (refreshResult.data) {
            setAssignmentConfig(refreshResult.data);
            setRowVersion(refreshResult.data.rowVersion);
            setOperatorCount(refreshResult.data.activeOperatorsPerDay);
            setDefaultOperator(refreshResult.data.defaultOperatorId);
            setSelectedOperatorIds(refreshResult.data.selectedOperatorIds ?? []);
          }
        } catch (reloadErr) {
          console.error('Failed to reload config:', reloadErr);
        }
      } else {
        showToast({ title: 'Error', text: errMsg, severity: 'error' });
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
      const applied = await api.applyStaffLeave({
        workforce_member_id: targetPos.workforce_member_id,
        scope_type: 'center', // Use center scope as default
        scope_id: targetPos.scope_id ?? '', // Use position's scope_id
        reason_code: 'planned_leave',
        starts_on: range.from,
        ends_on: range.to,
      });
      await api.approveStaffLeave(applied.data.leave.absence_id, {
        row_version: applied.data.leave.row_version,
      });

      // Optimistically update local state and refetch
      const newLeaves = [...(leaves[modalTarget] ?? []), range];
      setLeaves({
        ...leaves,
        [modalTarget]: mergeLeaves(newLeaves),
      });
      closeModal();
      showToast({ title: 'Leave added', text: 'operator availability updated', severity: 'success' });
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to add leave');
    }
  };

  const operatorsList = useMemo(() => positions.filter((p) => !p.is_backup_slot), [positions]);
  const operatorMemberIds = useMemo(
    () => operatorsList.map((op) => op.workforce_member_id).filter((id): id is string => !!id),
    [operatorsList]
  );

  const normalizeSelectedOperators = useCallback((ids: string[], count: number, fallbackDefault: string): string[] => {
    const available = new Set(operatorMemberIds);
    const next = ids.filter((id, index) => available.has(id) && ids.indexOf(id) === index).slice(0, count);
    if ((count === 1 || next.length === 0) && fallbackDefault && available.has(fallbackDefault) && !next.includes(fallbackDefault)) {
      next.unshift(fallbackDefault);
    }
    for (const id of operatorMemberIds) {
      if (next.length >= count) break;
      if (!next.includes(id)) next.push(id);
    }
    return next.slice(0, count);
  }, [operatorMemberIds]);

  const changeOperatorCount = (count: number) => {
    setOperatorCount(count);
    setSelectedOperatorIds((current) => normalizeSelectedOperators(current, count, defaultOperator));
  };

  const toggleSelectedOperator = (operatorId: string) => {
    setSelectedOperatorIds((current) => {
      if (current.includes(operatorId)) {
        return current.filter((id) => id !== operatorId);
      }
      if (current.length >= operatorCount) return current;
      return [...current, operatorId];
    });
  };

  // Get shift for an operator by workforce_member_id
  const getShiftForOperator = (workforceMemberId: string): VaccinationOperatorShift | undefined => {
    return shifts.find((s) => s.operatorId === workforceMemberId);
  };

  // Drive-operator ordering + weekly assignment preview — backend-config-driven
  // (N + default from config, PM fallback from config shifts, fallback chain ordered by shift)
  const weekOffOf = (op: Position): string => (op.week_off_weekday ?? op.week_off ?? '').toLowerCase();
  const firstName = (op?: Position): string => (op?.person_display_name ?? 'Operator').split(' ')[0];

  const orderedOps = useMemo(() => {
    const pinnedIds = operatorCount === 1 ? [defaultOperator] : selectedOperatorIds;
    const pinned = pinnedIds
      .map((id) => operatorsList.find((op) => op.workforce_member_id === id))
      .filter((op): op is Position => !!op);
    const pinnedPositionIds = new Set(pinned.map((op) => op.position_id));
    const rest = operatorsList.filter((op) => !pinnedPositionIds.has(op.position_id));
    return [...pinned, ...rest];
  }, [operatorsList, operatorCount, defaultOperator, selectedOperatorIds]);

  const weeklyPlan = useMemo(() => {
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
        // "First available wins": the covering operator IS the chosen op, so the reason must
        // name that same operator — never a separate PM-shift operator that isn't running the day.
        const fallbackName = firstName(op);
        const defName = def ? firstName(def) : 'Default';
        // Distinguish WHY the default dropped out: leave vs recurring week-off (mock wording).
        const defWhy = def && isOnLeave(def) ? 'on leave' : 'week-off';
        const verb = defWhy === 'on leave' ? 'covers' : 'fills';
        const kind = defWhy === 'on leave' ? ('warn' as const) : ('info' as const);
        return { dow, ops: chosen, reason: `${defName} ${defWhy} → ${fallbackName} ${verb}`, kind };
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
  }, [orderedOps, operatorCount, leaves]);

  const capForPosition = (pos: Position): number => pos.vaccination_daily_animal_cap ?? commonCap;

  const saveOperatorCap = async (pos: Position) => {
    if (!pos.position_id) return;
    const raw = (draftCaps[pos.position_id] ?? String(capForPosition(pos))).trim();
    const nextCap = Number(raw);
    if (!Number.isInteger(nextCap) || nextCap < 1 || nextCap > 200) {
      setCapError('Cap must be a whole number between 1 and 200 animals/day.');
      return;
    }
    setSavingCap(pos.position_id);
    setCapError(null);
    try {
      const api = getAdminApi();
      const response = await api.updateStaffPosition(pos.position_id, {
        row_version: pos.row_version,
        vaccination_daily_animal_cap: nextCap,
      });
      const updated = response.data.position as Position;
      setPositions((current) => current.map((item) => (item.position_id === updated.position_id ? { ...item, ...updated } : item)));
      setDraftCaps((current) => {
        const next = { ...current };
        delete next[pos.position_id!];
        return next;
      });
      showToast({ title: 'Saved', text: `${updated.person_display_name ?? 'Operator'} cap ${nextCap}/day`, severity: 'success' });
    } catch (err) {
      setCapError(err instanceof Error ? err.message : 'Failed to save operator cap');
    } finally {
      setSavingCap(null);
    }
  };

  const openCapEditor = () => {
    if (capConfigError) return; // never edit against a config that failed to load
    setDraftAnimalCap(animalShotCap == null ? '' : String(animalShotCap));
    setCapError(null);
    setCapEditing(true);
  };

  const saveCapacityConfig = async () => {
    if (capConfigError) {
      setCapError('Capacity config could not be loaded; reload before editing the cap.');
      return;
    }
    // This control edits ONLY the per-animal shot cap; the operator daily cap is owned by the
    // per-operator roster rows, so maxPerDay is sent back unchanged (current commonCap).
    // A cleared animal-cap field means "no override" (null → planner falls back to the rule DSL / default).
    // An explicit value must be a whole number >= 1; out-of-range is rejected, never silently defaulted.
    const animalRaw = draftAnimalCap.trim();
    let nextAnimal: number | null = null;
    if (animalRaw !== '') {
      const parsed = Number(animalRaw);
      if (!Number.isInteger(parsed) || parsed < 1) {
        setCapError('Animal shot cap must be a whole number ≥ 1, or blank to use the protocol default.');
        return;
      }
      nextAnimal = parsed;
    }
    setSavingCapCfg(true);
    setCapError(null);
    try {
      const api = getAdminApi();
      const response = await api.putVaccinationCapacityConfig({
        maxPerDay: commonCap,
        maxShotsPerAnimalPerDrive: nextAnimal,
        rowVersion: capRowVersion,
      });
      const updated = response.data;
      setCommonCap(updated.maxPerDay);
      setAnimalShotCap(updated.maxShotsPerAnimalPerDrive ?? null);
      setCapRowVersion(updated.rowVersion);
      setCapEditing(false);
      showToast({ title: 'Saved', text: 'animal shot cap updated — future vaccination schedules are being re-planned', severity: 'success' });
    } catch (err) {
      setCapError(err instanceof Error ? err.message : 'Could not save the capacity setting. Check the value and try again.');
    } finally {
      setSavingCapCfg(false);
    }
  };

  // KPIs
  const kpiOperators = operatorsList.length;
  const kpiDaily = operatorCount * commonCap;


  // Leave calendar (template MUI X DateCalendar, the calendar the template's CustomDateRangePicker
  // uses). Past days, days another operator booked and this operator's own planned days are not
  // pickable; the Day slot paints the span band, the ends and the two booked tints.
  const blocked = blockedDates(modalTarget || '', leaves);
  const own = ownDates(modalTarget || '', leaves);
  const today = todayISO();

  const pickLeaveDay = (ds: string) => {
    if (ds < today || blocked.has(ds) || own.has(ds)) return;
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
  };

  const LeaveDay = (props: PickerDayProps) => {
    const ds = props.day.format('YYYY-MM-DD');
    const isBlocked = blocked.has(ds);
    const isOwn = own.has(ds);
    const isEdge = ds === selFrom || ds === selTo;
    const inRange = Boolean(selFrom && selTo && ds > selFrom && ds < selTo);
    const tint = isBlocked ? 'error' : isOwn ? 'warning' : null;
    return (
      <PickerDay
        {...props}
        disabled={ds < today || isBlocked || isOwn}
        selected={isEdge && !props.outsideCurrentMonth}
        aria-pressed={isEdge}
        data-d={ds}
        sx={(theme) =>
          props.outsideCurrentMonth
            ? {}
            : tint
              ? {
                  bgcolor: varAlpha(theme.vars.palette[tint].mainChannel, 0.16),
                  border: 1,
                  borderColor: theme.vars.palette[tint].main,
                  '&.Mui-disabled': { color: theme.vars.palette.text.secondary },
                }
              : inRange
                ? { borderRadius: 0, bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.08) }
                : {}
        }
      />
    );
  };

  // Same shape as the tab-click fallback (people-skeletons): chooser card or roster, never a third shape.
  if (loading) return <VaccinationDeskSkeleton chooser={vaccinationDeskOpensOnChooser(chosenParkId ?? initialParkId, parks.length)} />;
  if (error)
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" role="alert">{error}</Alert>
      </Box>
    );

  // BUG-019: a caller whose authorized scope covers several parks must CHOOSE one before any roster,
  // capacity KPI, weekly preview, or default-operator dropdown is rendered — those are all park-scoped
  // and blending them was the defect. The options, their labels, and the reason copy are backend-owned
  // (409 `park_scope_ambiguous`); this screen only renders them and sends the chosen parkId back.
  // Nothing is preselected, so no local default can be silently overwritten by a later response.
  if (parkChoices) {
    return (
      <Stack spacing={3} data-skel-root="" data-screen="vaccination-operators">
        {/* Template Card + CardHeader with one select. Picking a park applies it (no Continue button
            that sits disabled until a pick: guard operators-park-chooser-applies). */}
        <Card>
          <CardHeader title="Choose a park" subheader={parkChoiceMessage || undefined} />
          <CardContent>
            {parkChoices.length === 0 ? (
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>No park is available for your access yet.</Typography>
            ) : (
              <MuiTextField
                select
                id="vaccination-operators-park-choice"
                label="Park"
                value={parkDraft}
                onChange={(event) => {
                  setParkDraft(event.target.value);
                  if (event.target.value) setChosenParkId(event.target.value);
                }}
                sx={{ minWidth: { xs: 1, sm: 260 }, maxWidth: 1 }}
                slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
              >
                <MenuItem value="" disabled>Select a park</MenuItem>
                {parkChoices.map((park) => (
                  <MenuItem key={park.parkId} value={park.parkId}>
                    {park.code ? `${park.code} · ${park.name}` : park.name}
                  </MenuItem>
                ))}
              </MuiTextField>
            )}
          </CardContent>
        </Card>
      </Stack>
    );
  }

  const drawerOp = drawerTarget ? operatorsList.find((p) => p.position_id === drawerTarget) : null;
  const drawerLeaves = drawerTarget ? (leaves[drawerTarget] ?? []) : [];
  const drawerUpcoming = drawerLeaves.filter((r) => r.to >= today).sort((a, b) => (a.from < b.from ? -1 : 1));
  const drawerPast = drawerLeaves.filter((r) => r.to < today).sort((a, b) => (a.from < b.from ? 1 : -1));

  // The park this screen is scoped to, named from the backend park list -- never a literal. Every
  // park added on Configuration > Items & settings is named here the same way.
  const scopedPark = parkId ? parks.find((p) => p.parkId === parkId) : undefined;
  const scopedParkName = scopedPark?.name ?? '';
  const scopedParkLabel = scopedPark ? [scopedPark.code, scopedPark.name].filter((part, i, all) => part && all.indexOf(part) === i).join(' · ') : '';

  const leaveItem = (r: { from: string; to: string }, past: boolean) => (
    <Box
      key={r.from}
      sx={{ px: 2, py: 1.5, borderRadius: 'var(--r-md)', border: 1, borderColor: 'divider', color: past ? 'text.disabled' : 'text.primary' }}
    >
      <Typography variant="subtitle2">{fmtRange(r)}</Typography>
      <Typography variant="caption" sx={{ color: past ? 'text.disabled' : 'text.secondary' }}>
        {daysIn(r)} day{daysIn(r) > 1 ? 's' : ''}
      </Typography>
    </Box>
  );

  return (
    <Stack spacing={3} data-skel-root="" data-screen="vaccination-operators">
      {/* The People page carries the title; this row names the park the screen is scoped to and,
          for a multi-park actor, keeps the backend park list as a Park switch (main 6259f5aed). */}
      {scopedParkLabel || (parkOptions && parkOptions.length > 1 && parkId) ? (
        <Stack direction="row" spacing={2} sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', rowGap: 1.5 }}>
          {scopedParkLabel ? <Caption>{scopedParkLabel}</Caption> : <span />}
          {parkOptions && parkOptions.length > 1 && parkId ? (
            <MuiTextField
              select
              id="vaccination-operators-park"
              label="Park"
              value={parkId}
              onChange={(e) => {
                if (e.target.value && e.target.value !== parkId) setChosenParkId(e.target.value);
              }}
              sx={{ minWidth: { xs: 1, sm: 220 }, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, htmlInput: { 'aria-label': 'Park scope' }, select: { MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {parkOptions.map((park) => (
                <MenuItem key={park.parkId} value={park.parkId}>
                  {park.code ? `${park.code} · ${park.name}` : park.name}
                </MenuItem>
              ))}
            </MuiTextField>
          ) : null}
        </Stack>
      ) : null}

      {/* KPI Row */}
      <KpiGrid min={210}>
        <KpiWidget title="Operators" total={kpiOperators} color="primary" caption="active vaccination seats" />
        <KpiWidget title="Cap / operator" total={commonCap} color="warning" caption="applies to all operators" icon="certificates" />
        <KpiWidget
          title="Operators / day"
          total={operatorCount}
          color="info"
          caption={operatorCount === 1 ? 'single + fallback' : operatorCount === 3 ? 'all parallel' : 'pair'}
        />
        <KpiWidget title="Daily capacity" total={kpiDaily} color="success" caption="at full availability" />
      </KpiGrid>

      {/* Roster & Availability: template table card (Card + CardHeader action, Scrollbar table). */}
      <Card>
        <CardHeader
          title="Operator roster & availability"
          sx={{ flexWrap: 'wrap', rowGap: 1.5, '& .MuiCardHeader-action': { alignSelf: 'center', m: 0 } }}
          action={
            /* Operator cap is edited per-operator in the roster rows below + summarized in the KPI
               card; this control edits ONLY the per-animal shot cap. */
            <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
              <Typography variant="body2" sx={{ color: 'text.secondary' }}>Animal shot cap</Typography>
              {capEditing ? (
                <>
                  <MuiTextField
                    size="small"
                    type="number"
                    placeholder="default"
                    value={draftAnimalCap}
                    onChange={(event) => setDraftAnimalCap(event.target.value)}
                    sx={{ width: 110 }}
                    slotProps={{ htmlInput: { 'aria-label': 'Per-animal shot cap per day', inputMode: 'numeric', min: 1 } }}
                  />
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>shots/animal</Typography>
                  <Button variant="contained" color="primary" size="small" disabled={savingCapCfg} onClick={() => void saveCapacityConfig()}>
                    {savingCapCfg ? 'Saving' : 'Save'}
                  </Button>
                  <Button variant="outlined" color="inherit" size="small" disabled={savingCapCfg} onClick={() => setCapEditing(false)}>
                    Cancel
                  </Button>
                </>
              ) : (
                <>
                  <Typography variant="subtitle2">{capConfigError ? '—' : animalShotCap == null ? 'default' : animalShotCap}</Typography>
                  <Typography variant="body2" sx={{ color: 'text.secondary' }}>shots/animal</Typography>
                  <Button
                    variant="outlined"
                    color="inherit"
                    size="small"
                    onClick={openCapEditor}
                    disabled={!!capConfigError}
                    aria-disabled={!!capConfigError}
                    title={capConfigError ? `Capacity config failed to load — editing disabled. ${capConfigError}` : 'Edit animal shot cap'}
                  >
                    Edit cap
                  </Button>
                </>
              )}
            </Stack>
          }
        />
        {capConfigError || capError ? (
          <Stack spacing={1.5} sx={{ px: 3, pt: 2 }}>
            {capConfigError ? (
              <Alert severity="error">
                Couldn’t load the vaccination capacity setting, so operator and animal caps can’t be edited right now. Reload to try again. ({capConfigError})
              </Alert>
            ) : null}
            {capError ? <Alert severity="error">{capError}</Alert> : null}
          </Stack>
        ) : null}
        <Scrollbar sx={{ mt: 3 }}>
          <Table sx={{ minWidth: ROSTER_MIN_WIDTH }}>
            <TableHeadCustom headCells={ROSTER_HEAD} />
            <TableBody>
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
                const positionId = op.position_id ?? '';
                const effectiveCap = capForPosition(op);
                const draftCap = draftCaps[positionId] ?? String(effectiveCap);
                const capChanged = draftCap.trim() !== String(effectiveCap);
                const shift = getShiftForOperator(op.workforce_member_id ?? '');

                return (
                  <TableRow key={op.position_id} hover>
                    <TableCell>
                      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                        <Avatar name={op.person_display_name ?? "OP"} initials={init} size={34} decorative />
                        <Box sx={{ minWidth: 0 }}>
                          <Typography variant="subtitle2" noWrap>{shortName}</Typography>
                          <Typography variant="caption" component="div" sx={{ color: 'text.secondary' }}>Vaccination operator</Typography>
                        </Box>
                      </Stack>
                    </TableCell>
                    <TableCell>{scopedParkName || '—'}</TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
                        {shift ? (
                          <Typography variant="body2">{shiftSummary(shift)}</Typography>
                        ) : (
                          <Typography variant="body2" sx={{ color: 'text.disabled' }}>Not set</Typography>
                        )}
                        <Button
                          color="primary"
                          size="small"
                          variant="soft"
                          onClick={() => openShiftForm(op)}
                          disabled={!op.workforce_member_id}
                          title={op.workforce_member_id ? undefined : 'This seat has no person yet'}
                        >
                          {shift ? 'Edit shift' : 'Set shift'}
                        </Button>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <MuiTextField
                          size="small"
                          type="number"
                          value={draftCap}
                          onChange={(event) => setDraftCaps((current) => ({ ...current, [positionId]: event.target.value }))}
                          sx={{ width: 92 }}
                          slotProps={{ htmlInput: { 'aria-label': `${shortName} animals/day cap`, inputMode: 'numeric', min: 1, max: 200 } }}
                        />
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>/day</Typography>
                        <IconButton
                          color={capChanged ? 'primary' : 'default'}
                          disabled={!capChanged || savingCap === positionId}
                          aria-busy={savingCap === positionId || undefined}
                          onClick={() => void saveOperatorCap(op)}
                          title={savingCap === positionId ? 'Saving' : capChanged ? 'Save cap' : 'Cap unchanged'}
                          aria-label={savingCap === positionId ? 'Saving' : `Save ${shortName} cap`}
                        >
                          <Iconify icon="eva:checkmark-fill" />
                        </IconButton>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Label color="info">{weekOffLabel}</Label>
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        {opLeaves.length ? (
                          <ButtonBase
                            onClick={() => openDrawer(op.position_id!)}
                            sx={{ display: 'block', textAlign: 'left', borderRadius: 'var(--r-sm)', px: 0.5, py: 0.25 }}
                          >
                            <Typography variant="subtitle2" sx={{ color: 'warning.main', whiteSpace: 'nowrap' }}>
                              {nextRange ? fmtRange(nextRange) : 'none upcoming'}
                            </Typography>
                            <Typography variant="caption" component="div" sx={{ color: 'text.secondary' }}>
                              {nextRange
                                ? `${opLeaves.length} planned${opUpcoming.length > 1 ? ` · ${opUpcoming.length - 1} more upcoming` : ''}`
                                : `${opLeaves.length} past`}
                            </Typography>
                          </ButtonBase>
                        ) : null}
                        <Button
                          size="small"
                          variant="outlined"
                          color="inherit"
                          startIcon={opLeaves.length ? undefined : <Iconify icon="mingcute:add-line" />}
                          onClick={() => openModal(op.position_id!)}
                          sx={{ whiteSpace: 'nowrap', flexShrink: 0 }}
                        >
                          {opLeaves.length ? 'Manage' : 'Add leave'}
                        </Button>
                      </Stack>
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={0.5}>
                        {WEEKDAYS.map((dow) => {
                          const isOff = weekOff.toLowerCase() === dow;
                          return (
                            <Stack key={dow} spacing={0.25} sx={{ alignItems: 'center', minWidth: 32 }}>
                              <Typography variant="caption" sx={{ color: 'text.secondary' }}>{WEEK_LABELS[dow]}</Typography>
                              <Label color={isOff ? 'default' : 'success'} sx={{ px: 0.5, minWidth: 30 }}>{isOff ? 'Off' : 'On'}</Label>
                            </Stack>
                          );
                        })}
                      </Stack>
                    </TableCell>
                    <TableCell>
                      {onLeaveToday ? (
                        <Label color="warning">On leave today</Label>
                      ) : weekOffToday ? (
                        <Label color="info">Week-off today</Label>
                      ) : (
                        <Label color="success">Available</Label>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Scrollbar>
      </Card>

      {/* Drive Operator Assignment */}
      <Card>
        <CardHeader title="Drive operator assignment" />
        <CardContent>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ alignItems: { xs: 'stretch', sm: 'center' }, flexWrap: 'wrap', rowGap: 2 }}>
            <MuiTextField
              select
              label="Active operators / day"
              value={String(operatorCount)}
              disabled={configSaving}
              title="Drives the live preview below."
              onChange={({ target: { value } }) => changeOperatorCount(parseInt(value, 10))}
              sx={{ minWidth: { xs: 0, sm: 190 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              <MenuItem value="1">1 operator</MenuItem>
              <MenuItem value="2">2 operators</MenuItem>
              <MenuItem value="3">3 operators</MenuItem>
            </MuiTextField>
            <MuiTextField
              select
              label="Default operator"
              value={defaultOperator}
              disabled={operatorCount !== 1 || configSaving}
              title={operatorCount !== 1 ? 'Parallel mode uses the selected operator cards below.' : 'CEO default. Drives the live preview.'}
              onChange={({ target: { value } }) => {
                setDefaultOperator(value);
                if (operatorCount === 1) setSelectedOperatorIds([value]);
              }}
              sx={{ minWidth: { xs: 0, sm: 190 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {operatorsList.map((op) => (
                <MenuItem key={op.workforce_member_id ?? op.position_id} value={op.workforce_member_id ?? op.position_id}>
                  {op.person_display_name || 'Operator'}
                </MenuItem>
              ))}
            </MuiTextField>
            <Button variant="contained" color="primary" onClick={persistOperatorConfig} disabled={configSaving} sx={{ flexShrink: 0 }}>
              Save configuration
            </Button>
          </Stack>
          <Typography variant="body2" sx={{ color: 'text.secondary', mt: 1.5 }}>
            Live preview of the assignment logic from the roster + week-offs. Configure and save active-operators
            and default operator settings to the backend.
          </Typography>
          {operatorCount !== 1 ? (
            <Box sx={{ mt: 2, display: 'grid', gap: 1.5, gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))', md: 'repeat(4, minmax(0, 1fr))', lg: 'repeat(5, minmax(0, 1fr))' } }}>
              {operatorsList.map((op) => {
                const operatorId = op.workforce_member_id ?? '';
                const checked = selectedOperatorIds.includes(operatorId);
                const disabled = configSaving || (!checked && selectedOperatorIds.length >= operatorCount);
                return (
                  <ToggleButton
                    key={op.position_id}
                    value={operatorId}
                    color="primary"
                    selected={checked}
                    disabled={disabled}
                    onChange={() => operatorId && toggleSelectedOperator(operatorId)}
                    title={disabled && !checked ? `Already selected ${operatorCount} operators` : `Toggle ${op.person_display_name ?? 'operator'}`}
                    sx={{ justifyContent: 'flex-start', gap: 1.5, px: 1.5, py: 1, textAlign: 'left', textTransform: 'none' }}
                  >
                    <Avatar name={op.person_display_name ?? 'OP'} initials={(op.person_display_name ?? 'OP')[0]} size={28} decorative />
                    <Box sx={{ minWidth: 0 }}>
                      <Typography variant="subtitle2" noWrap>{firstName(op)}</Typography>
                      <Typography variant="caption" component="div" sx={{ color: 'text.secondary' }}>off: {WEEK_LABELS[weekOffOf(op)] ?? '—'}</Typography>
                    </Box>
                  </ToggleButton>
                );
              })}
            </Box>
          ) : null}
          <Typography variant="subtitle2" sx={{ mt: 3 }}>
            {operatorCount === 1 ? 'Fallback chain — first available wins' : `Parallel — up to ${operatorCount}/day run together`}
          </Typography>
          <Stack direction="row" spacing={1} sx={{ mt: 1.5, alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
            {orderedOps.map((op, i) => {
              const isDefault = operatorCount === 1 && i === 0;
              const isDown = i >= operatorCount;
              return (
                <Fragment key={op.position_id}>
                  {i > 0 && (
                    <Typography variant="subtitle2" component="span" sx={{ color: 'text.disabled' }}>
                      {operatorCount === 1 ? '→' : '+'}
                    </Typography>
                  )}
                  <Stack
                    direction="row"
                    spacing={1}
                    sx={(theme) => ({
                      alignItems: 'center',
                      px: 1.5,
                      py: 1,
                      borderRadius: 'var(--r-md)',
                      border: 1,
                      borderStyle: isDown ? 'dashed' : 'solid',
                      borderColor: isDefault ? theme.vars.palette.primary.main : theme.vars.palette.divider,
                      bgcolor: isDefault ? varAlpha(theme.vars.palette.primary.mainChannel, 0.08) : 'transparent',
                    })}
                  >
                    <Avatar name={op.person_display_name ?? 'OP'} initials={(op.person_display_name ?? 'OP')[0]} size={28} decorative />
                    <Box>
                      <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
                        <Typography variant="subtitle2" sx={{ color: isDown ? 'text.secondary' : 'text.primary' }}>{firstName(op)}</Typography>
                        {isDefault && <Label color="primary">DEFAULT</Label>}
                      </Stack>
                      <Typography variant="caption" component="div" sx={{ color: 'text.secondary' }}>off: {WEEK_LABELS[weekOffOf(op)] ?? '—'}</Typography>
                    </Box>
                  </Stack>
                </Fragment>
              );
            })}
          </Stack>
          {operatorCount !== 1 ? (
            <Alert severity="info" sx={{ mt: 1.5 }}>
              <span>
                <b>Parallel mode:</b> selected operators run together. Week-off/leave drops that operator&apos;s slice for the day and the roster fills the open slot.
              </span>
            </Alert>
          ) : null}
          <Typography variant="subtitle2" sx={{ mt: 3 }}>
            Weekly assignment preview — who runs the drive each day
          </Typography>
        </CardContent>
        <Scrollbar>
          <Table size="small" sx={{ minWidth: 560 }}>
            <TableHeadCustom headCells={WEEKLY_HEAD} />
            <TableBody>
              {weeklyPlan.map((p) => (
                <TableRow key={p.dow}>
                  <TableCell>
                    <Typography variant="subtitle2">{WEEK_LABELS[p.dow]}</Typography>
                  </TableCell>
                  <TableCell>
                    {p.ops.length ? (
                      <Stack direction="row" spacing={2} sx={{ flexWrap: 'wrap', rowGap: 0.5 }}>
                        {p.ops.map((o) => (
                          <Stack key={o.position_id} direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
                            <Box component="span" sx={{ width: 'var(--sp-1)', height: 'var(--sp-1)', borderRadius: '50%', flexShrink: 0, bgcolor: `${KIND_COLOR[p.kind]}.main` }} />
                            <Typography variant="body2">{firstName(o)}</Typography>
                          </Stack>
                        ))}
                      </Stack>
                    ) : (
                      <Label color="error">— none —</Label>
                    )}
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" sx={{ color: 'text.secondary' }}>{p.reason}</Typography>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Scrollbar>
      </Card>

      {/* Right Drawer - Leave Details: template MinimalDrawer (portal, focus trap, Escape / scrim /
          X, focus back on the opener; Back closes it via useBackCloses). */}
      <MinimalDrawer
        open={drawerOpen && Boolean(drawerOp)}
        onClose={closeDrawer}
        title={drawerOp?.person_display_name || 'Operator'}
        aria-label={drawerOp?.person_display_name || 'Operator'}
        footer={
          <Button variant="contained" color="primary" fullWidth startIcon={<Iconify icon="mingcute:add-line" />} onClick={() => openModal(drawerTarget!)}>
            Add leave
          </Button>
        }
      >
        <Box sx={{ p: 2.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>Planned leave</Typography>
          {!drawerLeaves.length ? (
            <Typography variant="body2" sx={{ color: 'text.disabled', py: 2 }}>No planned leave. Use &quot;Add leave&quot;.</Typography>
          ) : (
            <>
              {drawerUpcoming.length > 0 && (
                <>
                  <Typography variant="overline" component="div" sx={{ color: 'text.secondary', mt: 1 }}>Upcoming</Typography>
                  {drawerUpcoming.map((r) => leaveItem(r, false))}
                </>
              )}
              {drawerPast.length > 0 && (
                <>
                  <Typography variant="overline" component="div" sx={{ color: 'text.secondary', mt: 1 }}>Past</Typography>
                  {drawerPast.map((r) => leaveItem(r, true))}
                </>
              )}
            </>
          )}
        </Box>
      </MinimalDrawer>

      {/* Modal - Set / Edit shift */}
      {(() => {
        const shiftOp = shiftTarget ? operatorsList.find((p) => p.position_id === shiftTarget) : undefined;
        const existing = shiftTarget ? getShiftForOperator(shiftOp?.workforce_member_id ?? '') : undefined;
        return (
          <Dialog fullWidth maxWidth="sm" open={Boolean(shiftTarget)} onClose={closeShiftForm} slotProps={{ paper: { "aria-label": existing ? 'Edit shift' : 'Set shift' } }}>
            <DialogTitle component="div" sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <Avatar name={shiftOp?.person_display_name || 'Operator'} size={36} decorative />
              <Box sx={{ minWidth: 0 }}>
                <Typography variant="h6" component="h3" id="shift-form-title">{existing ? 'Edit shift' : 'Set shift'}</Typography>
                <Typography variant="body2" sx={{ color: 'text.secondary' }}>{shiftOp?.person_display_name || 'Operator'} · {scopedParkName || 'Vaccination operator'}</Typography>
              </Box>
            </DialogTitle>
            <DialogContent sx={{ display: 'grid', gap: 2, pt: 1 }}>
              <Stack direction="row" spacing={2} sx={{ flexWrap: 'wrap', rowGap: 2, pt: 1 }}>
                <MuiTextField
                  select
                  label="Shift"
                  value={shiftDraft.shiftLabel}
                  disabled={shiftSaving}
                  onChange={({ target: { value } }) => setShiftDraft((d) => ({ ...d, shiftLabel: value }))}
                  sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
                  slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                >
                  <MenuItem value=''>— choose a shift —</MenuItem>
                  {SHIFT_LABEL_OPTIONS.map((o) => (
                    <MenuItem key={o.value} value={o.value}>
                      {o.label}
                    </MenuItem>
                  ))}
                </MuiTextField>
                <MuiTextField
                  label="Starts"
                  size="small"
                  placeholder="08:00"
                  value={shiftDraft.shiftStart}
                  onChange={(e) => setShiftDraft((d) => ({ ...d, shiftStart: e.target.value }))}
                  disabled={shiftSaving}
                  slotProps={{ htmlInput: { inputMode: "numeric", maxLength: 5 } }}
                />
                <MuiTextField
                  label="Ends"
                  size="small"
                  placeholder="17:00"
                  value={shiftDraft.shiftEnd}
                  onChange={(e) => setShiftDraft((d) => ({ ...d, shiftEnd: e.target.value }))}
                  disabled={shiftSaving}
                  slotProps={{ htmlInput: { inputMode: "numeric", maxLength: 5 } }}
                />
                <MuiTextField
                  select
                  label="Week off"
                  value={shiftDraft.weekOffWeekday}
                  disabled={shiftSaving}
                  onChange={({ target: { value } }) => setShiftDraft((d) => ({ ...d, weekOffWeekday: value }))}
                  sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
                  slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                >
                  {WEEK_OFF_OPTIONS.map((o) => (
                    <MenuItem key={o.value} value={o.value}>
                      {o.label}
                    </MenuItem>
                  ))}
                </MuiTextField>
              </Stack>
              <Caption>
                Times are 24-hour, like 08:00 or 17:30. The drive planner uses this shift and week off to
                decide who runs each day, so saving re-plans future vaccination drives for this park.
              </Caption>
              {shiftError && (
                <Alert severity="error" role="alert">
                  {shiftError}
                </Alert>
              )}
            </DialogContent>
            <DialogActions>
              {existing ? (
                clearArmed ? (
                  <>
                    <Typography variant="body2" sx={{ color: 'text.secondary', mr: 'auto' }}>Clear this shift?</Typography>
                    <Button size="small" variant="outlined" color="inherit" onClick={() => setClearArmed(false)} disabled={shiftSaving}>
                      Keep
                    </Button>
                    <Button size="small" variant="soft" color="error" onClick={() => void clearShift()} disabled={shiftSaving}>
                      {shiftSaving ? 'Clearing' : 'Clear shift'}
                    </Button>
                  </>
                ) : (
                  <Button size="small" variant="text" color="error" onClick={() => setClearArmed(true)} disabled={shiftSaving} sx={{ mr: 'auto' }}>
                    Clear shift
                  </Button>
                )
              ) : null}
              {!clearArmed && (
                <>
                  <Button size="small" variant="outlined" color="inherit" onClick={closeShiftForm} disabled={shiftSaving}>
                    Cancel
                  </Button>
                  <Button variant="contained" color="primary" size="small" onClick={() => void saveShift()} disabled={shiftSaving}>
                    {shiftSaving ? 'Saving' : 'Save shift'}
                  </Button>
                </>
              )}
            </DialogActions>
          </Dialog>
        );
      })()}

      {/* Modal - Add Leave: template MUI Dialog (custom-dialog layout), portalled above the drawer. */}
      {(() => {
        const leaveOpName = operatorsList.find((p) => p.position_id === modalTarget)?.person_display_name;
        return (
          <Dialog fullWidth maxWidth="xs" open={modalOpen} onClose={closeModal} slotProps={{ paper: { 'aria-label': 'Add planned leave' } }}>
            <DialogTitle component="div" sx={{ display: 'flex', alignItems: 'center', gap: 1.5, pr: 1.5 }}>
              <Avatar name={leaveOpName ?? 'OP'} initials={(leaveOpName ?? 'OP')[0]} size={32} decorative />
              <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                <Typography variant="h6" component="h3">Add planned leave</Typography>
                <Typography variant="body2" sx={{ color: 'text.secondary' }}>{leaveOpName || 'Operator'} · Vaccination operator</Typography>
              </Box>
              <IconButton onClick={closeModal} aria-label="Close">
                <Iconify icon="mingcute:close-line" />
              </IconButton>
            </DialogTitle>
            <DialogContent dividers sx={{ pt: 1 }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'baseline', justifyContent: 'space-between', flexWrap: 'wrap' }}>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>Pick leave dates</Typography>
                <Typography variant="subtitle2" id="lmRange">
                  {!selFrom ? '— pick a start day —' : !selTo ? `${fmtRange({ from: selFrom, to: selFrom })} → pick end` : fmtRange({ from: selFrom, to: selTo })}
                </Typography>
              </Stack>
              <DateCalendar
                value={selTo ? dayjs(selTo) : selFrom ? dayjs(selFrom) : null}
                referenceDate={dayjs(today)}
                onChange={(next) => {
                  if (next) pickLeaveDay(next.format('YYYY-MM-DD'));
                }}
                minDate={dayjs(today)}
                views={['day']}
                fixedWeekNumber={6}
                slots={{ day: LeaveDay }}
                slotProps={{ previousIconButton: { 'aria-label': 'Previous month' } as never, nextIconButton: { 'aria-label': 'Next month' } as never }}
                sx={{ width: 1, maxWidth: 1, height: 'auto' }}
              />
              <Stack direction="row" spacing={2} sx={{ flexWrap: 'wrap', rowGap: 1 }}>
                {LEAVE_LEGEND.map((item) => (
                  <Stack key={item.label} direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
                    <Box
                      component="span"
                      sx={(theme) => ({
                        width: 'var(--sp-1h)',
                        height: 'var(--sp-1h)',
                        borderRadius: 'var(--r-sm)',
                        bgcolor: item.solid ? theme.vars.palette[item.color].main : varAlpha(theme.vars.palette[item.color].mainChannel, 0.16),
                        border: item.solid ? 0 : 1,
                        borderColor: theme.vars.palette[item.color].main,
                      })}
                    />
                    <Typography variant="caption" sx={{ color: 'text.secondary' }}>{item.label}</Typography>
                  </Stack>
                ))}
              </Stack>
              {modalError && (
                <Alert severity="error" role="alert" sx={{ mt: 1.5 }}>
                  {modalError}
                </Alert>
              )}
            </DialogContent>
            <DialogActions>
              <Button size="small" variant="outlined" color="inherit" onClick={closeModal}>
                Cancel
              </Button>
              <Button size="small" variant="contained" color="primary" onClick={addLeave}>
                Add leave
              </Button>
            </DialogActions>
          </Dialog>
        );
      })()}

      {/* Save / error feedback: MUI Snackbar + filled Alert (the template's toast surface). */}
      <Snackbar
        open={toastOpen}
        autoHideDuration={TOAST_MS}
        onClose={(_event, reason) => {
          if (reason !== 'clickaway') setToastOpen(false);
        }}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        {toast ? (
          <Alert severity={toast.severity} variant="filled" onClose={() => setToastOpen(false)} sx={{ width: 1 }}>
            <span>
              {toast.title ? <b>{toast.title}</b> : null}
              {toast.title ? ' · ' : null}
              {toast.text}
            </span>
          </Alert>
        ) : (
          <span />
        )}
      </Snackbar>
    </Stack>
  );
}
