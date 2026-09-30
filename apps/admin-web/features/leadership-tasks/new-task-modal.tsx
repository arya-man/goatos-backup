"use client";

import { useCallback, useId, useRef, useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { AssigneePicker } from "@/components/assignee-picker";
import { ASSIGNEE_FIELD_SX } from "@/components/app/assignee-field-sx";
import { Iconify } from "@/components/minimal/iconify";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskAssignee } from "@/lib/api/server";
import { useBackCloses } from "@/components/use-back-closes";
import { TaskAttachmentPickers, TaskField, type TaskAttachmentPicker } from "./task-form-parts";
import { TaskDeadlineFields } from "./task-write-forms";
import { TASK_NO_ASSIGNEES_NOTE_BASIS, TASK_NO_ASSIGNEES_NOTE_HEIGHT } from "./tasks-layout";

/**
 * The "+ New task" entry on the web Tasks desk. Same shape as the phone's New task screen
 * (maintainer request 2026-09-11): For, Title, Brief, three attachment pickers, Send. It opens
 * as a modal from the button rather than sitting beside the list, and the modal is
 * client-local state -- no navigation, no document request. It is the template MUI Dialog
 * (portalled above the Ask Mesha button, focus trapped, body scroll locked): Escape / scrim / X
 * and browser Back close it and focus returns to the button.
 *
 * "For" is the Work Board's assignee picker in its form mode (`components/assignee-picker.tsx`,
 * `mode="single"`), not a native `<select>`. The select listed JOB TITLES ONLY -- "CEO / CXO" twice, two people
 * indistinguishable -- which is the dead `+5` chip in another costume: a CXO could not pick a
 * person by name. Every row now reads "Name — Title", typing matches either, and the hidden
 * `assignee_user_id` the Server Action reads is still posted, so the no-JS path is unchanged.
 * The required check moved with it: a hidden input cannot be `required`, so the form refuses a
 * submit with nobody chosen and says so beside the field with the backend's own sentence.
 */
export function NewTaskModal({
  assignees,
  action,
  returnTo,
  pageContract,
}: {
  assignees: LeadershipTaskAssignee[];
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
  /** The page's copy; when the host wires it, every string here is the backend's. */
  pageContract?: AdminUiPageContract;
}) {
  const [open, setOpen] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState("");
  const [title, setTitle] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [assigneeMissing, setAssigneeMissing] = useState(false);
  const [deadlineMissing, setDeadlineMissing] = useState(false);
  const [deadlineMin, setDeadlineMin] = useState("");
  const [picked, setPicked] = useState<Record<string, number>>({});
  const chosen = assignees.find((a) => a.user_id === assigneeId);
  const openerRef = useRef<HTMLButtonElement>(null);
  const forRef = useRef<HTMLDivElement>(null);
  const headingId = useId();
  const assigneeErrorId = useId();
  const text = (key: string, fallback: string) =>
    pageContract ? copy(pageContract, key, fallback) : fallback;

  const openModal = useCallback(() => {
    setIdempotencyKey(`admin-web-leadership-task:${crypto.randomUUID()}`);
    setTitle("");
    setAssigneeId("");
    setAssigneeMissing(false);
    setDeadlineMin(farmClockNow());
    setPicked({});
    setOpen(true);
  }, []);
  const closeModal = useCallback(() => {
    setOpen(false);
    openerRef.current?.focus();
  }, []);

  useBackCloses(open, closeModal);

  const pickers: TaskAttachmentPicker[] = [
    { key: "voice", label: text("picker.voice", "Voice note"), accept: "audio/*", icon: "solar:microphone-bold" },
    {
      key: "media",
      label: text("picker.media", "Photo or video"),
      accept: "image/*,video/*",
      icon: "solar:gallery-add-bold",
    },
    {
      key: "file",
      label: text("picker.file", "File"),
      accept: ".pdf,.doc,.docx,.xls,.xlsx,.csv,.txt",
      icon: "solar:file-text-bold",
    },
  ];

  return (
    <>
      {/* No assignable people (the assignees read came back empty): no grey, dead "New task" (DECIDED
          no-dead-controls; J2 P1-1). The action slot shows the backend reason as visible text instead,
          so nobody has to hover or tap to learn why there is nothing to press
          (guard: tasks-new-task-reason). */}
      {assignees.length ? (
        <Button
          ref={openerRef}
          type="button"
          variant="contained"
          color="primary"
          onClick={openModal}
          startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />}
          aria-haspopup="dialog"
          aria-expanded={open}
        >
          {text("new.open", "New task")}
        </Button>
      ) : (
        <Typography
          variant="body2"
          role="note"
          // Own row below md, 44px line (the loading twin's placeholder; guard: tasks-loading-mirror).
          sx={{ color: "text.secondary", display: "inline-flex", alignItems: "center", gap: 0.75, minHeight: TASK_NO_ASSIGNEES_NOTE_HEIGHT, flexBasis: TASK_NO_ASSIGNEES_NOTE_BASIS }}
        >
          <Iconify icon="eva:info-outline" width={18} sx={{ flexShrink: 0 }} />
          {text("new.no_assignees", "No one can be given a task from here yet.")}
        </Typography>
      )}
      <Dialog
        open={open}
        onClose={closeModal}
        fullWidth
        maxWidth="sm"
        scroll="paper"
        aria-labelledby={headingId}
        slotProps={{
          // `lt-modal` / `lt-modal-hd` are test hooks only (no stylesheet defines them).
          paper: { className: "lt-modal" },
          // The "For" picker is the first field: focus lands on it once the dialog has entered.
          transition: { onEntered: () => forRef.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus() },
        }}
      >
        <DialogTitle component="div" className="lt-modal-hd" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Iconify icon="mingcute:add-line" aria-hidden="true" sx={{ color: "primary.main", flex: "none" }} />
          <Typography variant="h6" component="h3" id={headingId} sx={{ flexGrow: 1 }}>{text("new.title", "New task")}</Typography>
          <IconButton
            type="button"
            onClick={closeModal}
            aria-label={text("action.close", "Close")}
            sx={{ mr: -1 }}
          >
            <Iconify icon="mingcute:close-line" aria-hidden="true" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers>
          <Box
            component="form"
            action={action}
            sx={{ display: "grid", gap: 3, py: 1 }}
            onSubmit={(event: React.FormEvent<HTMLFormElement>) => {
              // The person is the one required field the browser cannot check itself (a hidden
              // input is never validated), so the form checks it and says so in place.
              if (!assigneeId) {
                event.preventDefault();
                setAssigneeMissing(true);
                forRef.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
                return;
              }
              // The deadline is three parts (day, hour, minute) and the day picker is a hidden
              // input the browser cannot check, so the form checks all three and says so in one
              // sentence under the field, instead of the browser's bubble on the Hour box.
              const data = new FormData(event.currentTarget);
              const blank = ["deadline_date", "deadline_hour", "deadline_minute"].some(
                (key) => String(data.get(key) ?? "").trim() === "",
              );
              if (blank) {
                event.preventDefault();
                setDeadlineMissing(true);
                // Scrolled to, not focused: focusing the day field opens its calendar, which
                // adds a second red line under the field.
                event.currentTarget.querySelector<HTMLElement>(".lt-deadline")?.scrollIntoView({ block: "nearest" });
                return;
              }
              setDeadlineMissing(false);
            }}
          >
            <input type="hidden" name="idempotency_key" value={idempotencyKey} />
            <input type="hidden" name="return_to" value={returnTo} />
            <TaskField label={text("new.for_field", "For")} ref={forRef} sx={ASSIGNEE_FIELD_SX}>
              <AssigneePicker
                mode="single"
                name="assignee_user_id"
                labels={{
                  label: text("new.for_field", "For"),
                  placeholder: text("new.for_placeholder", "Choose who this is for"),
                  search: text("filter.people_search", "Type a name"),
                  none: text("filter.people_no_matches", "Nobody by that name."),
                }}
                owners={assignees.map((assignee) => ({
                  id: assignee.user_id,
                  name: assignee.name,
                  title: assignee.title,
                }))}
                selected={assigneeId || undefined}
                onSelect={(next) => {
                  setAssigneeId(next ?? "");
                  if (next) setAssigneeMissing(false);
                }}
                invalid={assigneeMissing}
                describedBy={assigneeMissing ? assigneeErrorId : undefined}
              />
              {assigneeMissing ? (
                <Typography id={assigneeErrorId} variant="caption" role="alert" sx={{ color: "error.main" }}>
                  {text("feedback.missing_assignee", "Choose who the task is for.")}
                </Typography>
              ) : chosen ? (
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {text("new.assigned_to", "Assigned to")}{" "}
                  <Box component="b" sx={{ color: "text.primary" }}>{chosen.name}</Box>
                  {chosen.title ? <> — {chosen.title}</> : null}
                </Typography>
              ) : null}
            </TaskField>
            <TextField
              fullWidth
              name="title"
              label={text("new.title_field", "Title")}
              required
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              helperText={`${title.length} / 80`}
              slotProps={{ htmlInput: { maxLength: 80 }, inputLabel: { shrink: true }, formHelperText: { sx: { textAlign: "right", mx: 0 } } }}
            />
            <TextField
              fullWidth
              multiline
              rows={4}
              name="body"
              label={text("new.body_field", "Brief")}
              slotProps={{ htmlInput: { maxLength: 4000 }, inputLabel: { shrink: true } }}
            />
            <TaskDeadlineFields
              pageContract={pageContract}
              defaultLocal=""
              min={deadlineMin.slice(0, 10) || undefined}
              required
              missing={deadlineMissing}
              label={text("new.deadline_field", "Deadline")}
              hint={text("new.deadline_hint", "Date and time the task is due, farm clock (IST).")}
            />
            <TaskField label={text("new.attachments", "Attachments")}>
              <TaskAttachmentPickers
                pickers={pickers}
                counts={picked}
                onPicked={(key, count) => setPicked((prev) => ({ ...prev, [key]: count }))}
              />
            </TaskField>
            <Button type="submit" variant="contained" color="primary" size="large" fullWidth>
              {text("new.send", "Send")}
            </Button>
          </Box>
        </DialogContent>
      </Dialog>
    </>
  );
}

/**
 * The earliest deadline the calendar offers: now, on the farm's clock (Asia/Kolkata), in the
 * `YYYY-MM-DDTHH:MM` form `deadline.ts` speaks; the day half bounds the calendar. The browser may
 * sit anywhere; the deadline is always read as IST.
 */
function farmClockNow(): string {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(new Date());
  const get = (type: string) =>
    parts.find((part) => part.type === type)?.value ?? "";
  const hour = get("hour") === "24" ? "00" : get("hour");
  return `${get("year")}-${get("month")}-${get("day")}T${hour}:${get("minute")}`;
}
