"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

/**
 * Read-only view of one earlier version's settings.
 *
 * Every published plan is kept, so a person must be able to answer "what did we
 * actually have in force last March?" without a restore. This sheet is that
 * answer and is strictly read-only: a retired version is immutable in the
 * database, so offering any control that appears to change it would be a lie.
 *
 * The vaccine table here is the SAME projection the live card uses
 * (plan-model.groupSchedule), so the two can never drift into disagreeing about
 * what a plan said.
 */

import { X } from "lucide-react";
import { useEffect } from "react";

import { describeFirstDoses, describeRepeats, type VaccineGroup } from "./plan-model";

export type VersionSheetData = {
  label: string;
  inForce: string;
  published: string;
  vaccines: VaccineGroup[];
};

type Props = {
  data: VersionSheetData | null;
  loading: boolean;
  error: string | null;
  onClose: () => void;
};

export function VersionSheet({ data, loading, error, onClose }: Props) {
  // Escape closes. A modal that traps the reader with no keyboard exit is a
  // defect, not a detail.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="vp-modal"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="vp-sheet" role="dialog" aria-modal="true" aria-label="Version settings">
        <div className="vp-head">
          <div>
            <div className="eyebrow" style={{ marginBottom: 4 }}>
              Read-only
            </div>
            <h2>{data?.label ?? "Version settings"}</h2>
          </div>
          <button className="vp-x" onClick={onClose} type="button" aria-label="Close">
            {/* lucide, not a dingbat: check-mock-fidelity.mjs rejects emoji glyphs
                because the mock uses real icons and a dingbat renders differently
                on every platform. */}
            <X size={15} aria-hidden />
          </button>
        </div>
        <div className="vp-body">
          {loading ? <p className="vp-lead">Loading…</p> : null}
          {error ? <p className="vp-lead">{error}</p> : null}
          {data && !loading && !error ? (
            <>
              <p className="vp-lead">
                In force <b>{data.inForce}</b> · published <b>{data.published}</b>. This version is
                retired and cannot be changed. To bring any of it back, start a new version.
              </p>
              <div className="scroll" style={{ marginTop: 0 }}>
                <Table className="tabl">
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">Vaccine</TableCell>
                      <TableCell component="th">First doses</TableCell>
                      <TableCell component="th">Repeats</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {data.vaccines.map((v) => (
                      <TableRow className={v.inPlan ? undefined : "voff"} key={v.code}>
                        <TableCell>
                          <b>{v.name}</b>
                        </TableCell>
                        <TableCell>{v.inPlan ? describeFirstDoses(v.firstDoses) : "—"}</TableCell>
                        <TableCell>{v.inPlan ? describeRepeats(v.repeats) : "—"}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}
