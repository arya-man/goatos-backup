"use client";

import { FileText, Image, Mic, Paperclip, Pencil, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { rfc3339ToFarmDeadlineLocal } from "./deadline";
import { TaskDeadlineFields } from "./task-write-forms";
import { useDialogShell } from "./use-dialog-shell";
import { attachmentKindLabel, type TaskRow } from "./task-row";
import { useTaskRowVersion, useTaskWriteInFlight } from "./task-row-store";

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
  const dialogRef = useRef<HTMLDivElement>(null);
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

  // Escape, the body scroll lock and the focus trap all live in the shared hook, so the modal and
  // the filter sheet cannot drift apart on a phone.
  useDialogShell({ open, onClose: closeModal, containerRef: dialogRef });

  useEffect(() => {
    if (!open) return;
    mintKey();
    firstFieldRef.current?.focus();
  }, [open, mintKey]);

  const pickers: Array<{ key: string; label: string; accept: string; icon: typeof Mic }> = [
    { key: "voice", label: copy(pageContract, "picker.voice"), accept: "audio/*", icon: Mic },
    { key: "media", label: copy(pageContract, "picker.media"), accept: "image/*,video/*", icon: Image },
    {
      key: "file",
      label: copy(pageContract, "picker.file"),
      accept: ".pdf,.doc,.docx,.xls,.xlsx,.csv,.txt",
      icon: FileText,
    },
  ];

  const keptCount = task.attachmentRows.filter((attachment) => kept[attachment.proof_id]).length;
  const addedCount = Object.values(added).reduce((sum, count) => sum + count, 0);
  const overLimit = keptCount + addedCount > 12;

  return (
    <>
      <button
        ref={openerRef}
        type="button"
        className="btn"
        onClick={openModal}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <Pencil className="ic" aria-hidden="true" />
        {copy(pageContract, "action.edit")}
      </button>
      {open ? (
        <>
          <button
            type="button"
            className="scrim on lt-modal-scrim"
            aria-label={copy(pageContract, "action.close")}
            onClick={closeModal}
          />
          <div ref={dialogRef} className="lt-modal" role="dialog" aria-modal="true" aria-labelledby={headingId}>
            <div className="lt-modal-hd">
              <Pencil className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3 id={headingId}>
                {copy(pageContract, "edit.title")} {task.number}
              </h3>
              <div className="sp" style={{ flex: 1 }} />
              <button
                type="button"
                className="btn"
                onClick={closeModal}
                aria-label={copy(pageContract, "action.close")}
              >
                <X className="ic" aria-hidden="true" />
              </button>
            </div>
            <form action={action} className="lt-modal-bd">
              <input ref={keyRef} type="hidden" name="idempotency_key" />
              <input type="hidden" name="return_to" value={returnTo} />
              <input type="hidden" name="task_id" value={task.id} />
              {/* The version of the row that is ON SCREEN. */}
              <input type="hidden" name="row_version" value={rowVersion} />
              <label className="fld">
                <span>{copy(pageContract, "edit.title_field")}</span>
                <input
                  ref={firstFieldRef}
                  name="title"
                  required
                  maxLength={80}
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                />
                <small className="lt-counter">{title.length} / 80</small>
              </label>
              <label className="fld">
                <span>{copy(pageContract, "edit.body_field")}</span>
                <textarea
                  name="body"
                  maxLength={4000}
                  rows={4}
                  value={body}
                  onChange={(event) => setBody(event.target.value)}
                />
              </label>
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
              <div className="fld">
                <span className="lt-fld-label">{copy(pageContract, "edit.attachments")}</span>
                {task.attachmentRows.length ? (
                  <div className="lt-keeplist">
                    {task.attachmentRows.map((attachment) => (
                      <label key={attachment.proof_id} className="lt-keep">
                        <input
                          type="checkbox"
                          checked={Boolean(kept[attachment.proof_id])}
                          onChange={(event) =>
                            setKept((prev) => ({
                              ...prev,
                              [attachment.proof_id]: event.target.checked,
                            }))
                          }
                        />
                        <Paperclip className="ic" aria-hidden="true" />
                        <span>{attachment.file_name || attachmentKindLabel(attachment.kind)}</span>
                        {/* Re-posted as a ref only while it is ticked: the list the server receives
                            IS the new list, so an unticked file is removed by being absent. */}
                        {kept[attachment.proof_id] ? (
                          <>
                            <input type="hidden" name="attachment_proof_id" value={attachment.proof_id} />
                            <input type="hidden" name="attachment_kind" value={attachment.kind} />
                            <input
                              type="hidden"
                              name="attachment_file_name"
                              value={attachment.file_name ?? ""}
                            />
                          </>
                        ) : null}
                      </label>
                    ))}
                  </div>
                ) : null}
                <div className="lt-pickers">
                  {pickers.map((picker) => {
                    const Icon = picker.icon;
                    const count = added[picker.key] ?? 0;
                    return (
                      <label key={picker.key} className="btn lt-picker">
                        <Icon className="ic" aria-hidden="true" />
                        <span className="lt-picker-label">{picker.label}</span>
                        {count ? <span className="cbq">{count}</span> : null}
                        <input
                          type="file"
                          name="attachment_file"
                          multiple
                          accept={picker.accept}
                          onChange={(event) =>
                            setAdded((prev) => ({
                              ...prev,
                              [picker.key]: event.target.files?.length ?? 0,
                            }))
                          }
                        />
                      </label>
                    );
                  })}
                </div>
                <small className="lt-counter">
                  {keptCount + addedCount} / 12
                </small>
                {overLimit ? (
                  <small className="lt-fnote">{copy(pageContract, "edit.too_many")}</small>
                ) : null}
              </div>
              <button
                type="submit"
                className="btn p lt-send"
                disabled={overLimit || writing}
                aria-busy={writing || undefined}
                // Re-minted on the submit gesture itself, so a modal left open across a
                // back/forward navigation cannot post the key its opening minted.
                onClick={mintKey}
              >
                {copy(pageContract, "edit.save")}
              </button>
            </form>
          </div>
        </>
      ) : null}
    </>
  );
}
