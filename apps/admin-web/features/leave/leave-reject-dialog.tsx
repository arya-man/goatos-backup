"use client";

import { useState } from "react";
import { Iconify } from "@/components/minimal/iconify";
import Dialog from "@mui/material/Dialog";
import Button from "@mui/material/Button";
import Box from "@mui/material/Box";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

export type LeaveRejectDialogProps = {
  leaveRequestId: string;
  personName: string;
  datesLabel: string;
  returnTo: string;
  action: (formData: FormData) => void | Promise<void>;
  labels: {
    trigger: string;
    title: string;
    body: string;
    reason: string;
    reasonHint: string;
    cancel: string;
    confirm: string;
  };
};

/**
 * Rejecting someone's leave is destructive and used to be one mis-click away: a bare, unlabelled
 * `<input name="reason">` sat inside the table row with the Reject button beside it. It now opens a
 * kit `Dialog` (the pattern the routine-retire confirm already uses) with a labelled, required
 * reason field, a character counter and an explicit confirm in the danger tone. Nothing is
 * submitted until the confirm inside the dialog is pressed.
 */
export function LeaveRejectDialog({ leaveRequestId, personName, datesLabel, returnTo, action, labels }: LeaveRejectDialogProps) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const MAX = 500;
  return (
    <>
      <Button size="small" variant="soft" color="error" data-testid="leave-reject" onClick={() => setOpen(true)}>
        {labels.trigger}
      </Button>
      <Dialog fullWidth maxWidth="xs" open={open} onClose={() => setOpen(false)} slotProps={{ paper: { "aria-label": `${labels.title}: ${personName}` } }}>
        <form action={action} onSubmit={() => setOpen(false)}>
          <input type="hidden" name="leave_request_id" value={leaveRequestId} />
          <input type="hidden" name="return_to" value={returnTo} />
          {/* Template custom-dialog ConfirmDialog layout: title, body2 content, actions. */}
          <DialogTitle component="div" sx={{ display: "flex", alignItems: "flex-start", gap: 1.5, pb: 1 }}>
            <Box component="span" aria-hidden="true" sx={{ display: "inline-flex", color: "error.main", pt: 0.25 }}><Iconify icon="solar:danger-bold" width={20} /></Box>
            <Box sx={{ minWidth: 0 }}>
              <Typography variant="h6" component="div">{labels.title}</Typography>
              <Typography variant="body2" sx={{ color: "text.secondary" }}>{personName} · {datesLabel}</Typography>
            </Box>
          </DialogTitle>
          <DialogContent sx={{ typography: "body2" }}>
            <Typography variant="body2" sx={{ color: "text.secondary", mb: 2 }}>{labels.body}</Typography>
            <TextField
              name="reason"
              label={labels.reason}
              required
              fullWidth
              multiline
              minRows={3}
              value={reason}
              placeholder={labels.reasonHint}
              onChange={(e) => setReason(e.target.value)}
              helperText={
                <Box component="span" id={`lv-reason-help-${leaveRequestId}`} sx={{ display: "flex", gap: 1, alignItems: "baseline" }}>
                  <span>{labels.reasonHint}</span>
                  <Box component="span" sx={{ ml: "auto", fontVariantNumeric: "tabular-nums" }}>{reason.length}/{MAX}</Box>
                </Box>
              }
              slotProps={{ htmlInput: { maxLength: MAX, "aria-describedby": `lv-reason-help-${leaveRequestId}` }, inputLabel: { shrink: true } }}
            />
          </DialogContent>
          <DialogActions>
            <Button color="primary" type="button" variant="outlined" onClick={() => setOpen(false)}>{labels.cancel}</Button>
            <Box sx={{ flex: 1 }} />
            <Button variant="contained" type="submit" color="error" startIcon={<Iconify icon="solar:danger-bold" width={16} />} disabled={reason.trim().length === 0}>
              {labels.confirm}
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </>
  );
}
