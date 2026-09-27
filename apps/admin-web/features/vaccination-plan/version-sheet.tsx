"use client";
import Table from "@mui/material/Table";
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

import Alert from "@mui/material/Alert";
import Paper from "@mui/material/Paper";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";

import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";

import { describeFirstDoses, describeRepeats, type VaccineGroup } from "./plan-model";

export type VersionSheetData = {
  label: string;
  inForce: string;
  published: string;
  vaccines: VaccineGroup[];
};

type Props = {
  open: boolean;
  data: VersionSheetData | null;
  loading: boolean;
  error: string | null;
  onClose: () => void;
};

// Template Dialog (portal, backdrop, Escape and backdrop close, fullScreen on phones). The root carries
// the `vp-version-sheet` class the E2E suite (tools/e2e/suite.mjs) looks for.
export function VersionSheet({ open, data, loading, error, onClose }: Props) {
  return (
    <Dialog
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="md"
      className="vp-version-sheet"
      slotProps={{ paper: { "aria-label": "Version settings" } }}
      sx={{ "& .MuiDialog-container": { alignItems: { xs: "flex-end", sm: "center" } } }}
    >
      <DialogTitle sx={{ pr: 7 }}>
        <Typography variant="overline" component="div" sx={{ color: "text.secondary" }}>
          Read-only
        </Typography>
        {data?.label ?? "Version settings"}
        <IconButton onClick={onClose} aria-label="Close" sx={{ position: "absolute", top: 12, right: 12 }}>
          <Iconify icon="mingcute:close-line" />
        </IconButton>
      </DialogTitle>
      <DialogContent sx={{ pb: 3 }}>
        {loading ? <Typography variant="body2" sx={{ color: "text.secondary" }}>Loading…</Typography> : null}
        {error ? <Alert severity="error">{error}</Alert> : null}
        {data && !loading && !error ? (
          <>
            <Typography variant="body2" sx={{ color: "text.secondary", mb: 2 }}>
              In force <b>{data.inForce}</b> · published <b>{data.published}</b>. This version is retired and cannot be changed. To bring any of it back, start a new version.
            </Typography>
            <Paper variant="outlined" sx={{ overflow: "hidden" }}>
              <Scrollbar>
                <Table sx={{ minWidth: 560 }}>
                  <TableHeadCustom
                    headCells={[
                      { id: "vaccine", label: "Vaccine" },
                      { id: "first", label: "First doses" },
                      { id: "repeats", label: "Repeats" },
                    ]}
                  />
                  <TableBody>
                    {data.vaccines.map((v) => (
                      <TableRow key={v.code} sx={v.inPlan ? undefined : { "& td": { color: "text.disabled" } }}>
                        <TableCell sx={{ typography: "subtitle2" }}>{v.name}</TableCell>
                        <TableCell>{v.inPlan ? describeFirstDoses(v.firstDoses) : "—"}</TableCell>
                        <TableCell>{v.inPlan ? describeRepeats(v.repeats) : "—"}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Scrollbar>
            </Paper>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
