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

import { Iconify } from "@/components/minimal/iconify";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { rfc3339ToFarmDeadlineLocal } from "./deadline";
import { TaskAttachmentPickers, TaskField, type TaskAttachmentPicker } from "./task-form-parts";
import { TaskDeadlineFields } from "./task-write-forms";
import { useBackCloses } from "@/components/use-back-closes";
import { attachmentKindLabel, type TaskRow } from "./task-row";
import { useTaskRowVersion, useTaskWriteInFlight } from "./task-row-store";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

/**
 * The EDIT modal — the piece the web desk never had.
 *
 * `POST /app/leadership-tasks/{task_id}/edit` has existed all along with no web client, which is
 * what "task editing is not working" was. Every rule is the server's and this form only has to
 * feed it honestly:
 *   - RAISER ONLY, and only while the task is open or in_progress. The opener is not rendered
 *     unless the row says `can_edit`, so the reader is never offered a write that will 409.
 *   - ROW_VERSION FENCE. The version travels from the row that is on screen; a stale one comes
 *     back as `version_conflict` and the page says so rather than appearing to succeed.
 *   - AN OMITTED DEADLINE KEEPS THE STORED ONE. The field is pre-filled with the stored deadline
 *     on the farm's clock, and clearing it sends nothing at all rather than a blank instant.
 *   - ATTACHMENTS ARE A FULL REPLACEMENT LIST, max 12. The files the reader keeps are re-posted
 *     as refs; unticking one is how it is removed.
 *
 * The idempotency key is minted HERE, in the browser, on the gesture that opens the modal and
 * again on the gesture that submits — never during a server render. A server-rendered key is
 * replayed verbatim by a back navigation, and the second, legitimate edit is then swallowed as a
 * duplicate of the first.
 */
export function EditTaskModal({
  task,
  pageContract,
  action,
  returnTo,
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  const rowVersion = useTaskRowVersion(task.id, task.rowVersion);
  // Save waits for a comment or status change to this task still on the wire: the fence below is
  // the version ON SCREEN, and the in-flight write is about to move it.
  const writing = useTaskWriteInFlight(task.id);
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState(task.title);
  const [body, setBody] = useState(task.body);
  const [deadline, setDeadline] = useState("");
  const [kept, setKept] = useState<Record<string, boolean>>({});
  const [added, setAdded] = useState<Record<string, number>>({});
  const openerRef = useRef<HTMLButtonElement>(null);
  const firstFieldRef = useRef<HTMLInputElement>(null);
  const keyRef = useRef<HTMLInputElement>(null);
  const headingId = useId();

  const mintKey = useCallback(() => {
    if (keyRef.current) {
      keyRef.current.value = `admin-web-leadership-task-edit:${task.id}:${crypto.randomUUID()}`;
    }
  }, [task.id]);

  const openModal = useCallback(() => {
    setTitle(task.title);
    setBody(task.body);
    setDeadline(rfc3339ToFarmDeadlineLocal(task.deadlineAt));
    setKept(
      Object.fromEntries(task.attachmentRows.map((attachment) => [attachment.proof_id, true])),
    );
    setAdded({});
    setOpen(true);
  }, [task]);

  const closeModal = useCallback(() => {
    setOpen(false);
    openerRef.current?.focus();
  }, []);

  // Template MUI Dialog owns Escape, the scroll lock and the focus trap; Back closes it too.
  useBackCloses(open, closeModal);

  const pickers: TaskAttachmentPicker[] = [
    { key: "voice", label: copy(pageContract, "picker.voice"), accept: "audio/*", icon: "solar:microphone-bold" },
    { key: "media", label: copy(pageContract, "picker.media"), accept: "image/*,video/*", icon: "solar:gallery-add-bold" },
    {
      key: "file",
      label: copy(pageContract, "picker.file"),
      accept: ".pdf,.doc,.docx,.xls,.xlsx,.csv,.txt",
      icon: "solar:file-text-bold",
    },
  ];

  const keptCount = task.attachmentRows.filter((attachment) => kept[attachment.proof_id]).length;
  const addedCount = Object.values(added).reduce((sum, count) => sum + count, 0);
  const overLimit = keptCount + addedCount > 12;

  return (
    <>
      <Button
        ref={openerRef}
        type="button"
        variant="outlined"
        color="inherit"
        size="small"
        onClick={openModal}
        aria-haspopup="dialog"
        aria-expanded={open}
        startIcon={<Iconify icon="solar:pen-bold" aria-hidden="true" />}
        sx={{ minHeight: { xs: 44, sm: 36 } }}
      >
        {copy(pageContract, "action.edit")}
      </Button>
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
          // Minted (and the first field focused) once the dialog has mounted its portal content.
          transition: {
            onEntering: mintKey,
            onEntered: () => firstFieldRef.current?.focus(),
          },
        }}
      >
        <DialogTitle component="div" className="lt-modal-hd" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Iconify icon="solar:pen-bold" aria-hidden="true" sx={{ color: "primary.main", flex: "none" }} />
          <Typography variant="h6" component="h3" id={headingId} sx={{ flexGrow: 1 }}>
            {copy(pageContract, "edit.title")} {task.number}
          </Typography>
          <IconButton type="button" onClick={closeModal} aria-label={copy(pageContract, "action.close")} sx={{ mr: -1 }}>
            <Iconify icon="mingcute:close-line" aria-hidden="true" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers>
          <Box component="form" action={action} sx={{ display: "grid", gap: "var(--sp-3)", py: 1 }}>
            <input ref={keyRef} type="hidden" name="idempotency_key" />
            <input type="hidden" name="return_to" value={returnTo} />
            <input type="hidden" name="task_id" value={task.id} />
            {/* The version of the row that is ON SCREEN. */}
            <input type="hidden" name="row_version" value={rowVersion} />
            <TextField
              fullWidth
              inputRef={firstFieldRef}
              name="title"
              label={copy(pageContract, "edit.title_field")}
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
              label={copy(pageContract, "edit.body_field")}
              value={body}
              onChange={(event) => setBody(event.target.value)}
              slotProps={{ htmlInput: { maxLength: 4000 }, inputLabel: { shrink: true } }}
            />
            {/* Pre-filled with the stored deadline on the farm's clock; left as it is, the
                Server Action posts the same instant back, which the backend reads as unchanged.
                The state hook above is kept as the one place the value is minted on open. */}
            <TaskDeadlineFields
              pageContract={pageContract}
              defaultLocal={deadline}
              required={false}
              label={copy(pageContract, "edit.deadline_field")}
              hint={copy(pageContract, "edit.deadline_hint")}
            />
            <TaskField label={copy(pageContract, "edit.attachments")}>
              {task.attachmentRows.length ? (
                <Box sx={{ display: "grid", gap: 0.5 }}>
                  {task.attachmentRows.map((attachment) => (
                    <FormControlLabel
                      key={attachment.proof_id}
                      control={
                        <Checkbox
                          checked={Boolean(kept[attachment.proof_id])}
                          onChange={(event) =>
                            setKept((prev) => ({
                              ...prev,
                              [attachment.proof_id]: event.target.checked,
                            }))
                          }
                          sx={{ p: { xs: 1.5, sm: 1 } }}
                        />
                      }
                      sx={{ mx: 0, gap: 0.5, minWidth: 0 }}
                      label={
                        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1, minWidth: 0, typography: "body2" }}>
                          <Iconify icon="eva:attach-2-fill" width={16} aria-hidden="true" sx={{ flex: "none", color: "text.secondary" }} />
                          <Box component="span" sx={{ minWidth: 0, overflowWrap: "anywhere" }}>
                            {attachment.file_name || attachmentKindLabel(attachment.kind)}
                          </Box>
                          {/* Re-posted as a ref only while it is ticked: the list the server receives
                              IS the new list, so an unticked file is removed by being absent. */}
                          {kept[attachment.proof_id] ? (
                            <>
                              <input type="hidden" name="attachment_proof_id" value={attachment.proof_id} />
                              <input type="hidden" name="attachment_kind" value={attachment.kind} />
                              <input type="hidden" name="attachment_file_name" value={attachment.file_name ?? ""} />
                            </>
                          ) : null}
                        </Box>
                      }
                    />
                  ))}
                </Box>
              ) : null}
              <TaskAttachmentPickers
                pickers={pickers}
                counts={added}
                onPicked={(key, count) => setAdded((prev) => ({ ...prev, [key]: count }))}
              />
              <Typography variant="caption" sx={{ justifySelf: "end", color: "text.secondary" }}>
                {keptCount + addedCount} / 12
              </Typography>
              {overLimit ? (
                <Typography variant="caption" role="alert" sx={{ color: "error.main" }}>
                  {copy(pageContract, "edit.too_many")}
                </Typography>
              ) : null}
            </TaskField>
            <Button
              type="submit"
              variant="contained"
              color="primary"
              size="large"
              fullWidth
              disabled={overLimit || writing}
              aria-busy={writing || undefined}
              // Re-minted on the submit gesture itself, so a modal left open across a
              // back/forward navigation cannot post the key its opening minted.
              onClick={mintKey}
            >
              {copy(pageContract, "edit.save")}
            </Button>
          </Box>
        </DialogContent>
      </Dialog>
    </>
  );
}
