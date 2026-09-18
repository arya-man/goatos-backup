"use client";

import { FileText, Image, Mic, Plus, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import { AssigneePicker } from "@/components/assignee-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskAssignee } from "@/lib/api/server";
import { TaskDeadlineFields } from "./task-write-forms";
import { useDialogShell } from "./use-dialog-shell";

/**
 * The "+ New task" entry on the web Tasks desk. Same shape as the phone's New task screen
 * (maintainer request 2026-09-11): For, Title, Brief, three attachment pickers, Send. It opens
 * as a modal from the button rather than sitting beside the list, and the modal is
 * client-local state -- no navigation, no document request, Escape / scrim / X close it and
 * focus returns to the button.
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
  const [deadlineMin, setDeadlineMin] = useState("");
  const [picked, setPicked] = useState<Record<string, number>>({});
  const chosen = assignees.find((a) => a.user_id === assigneeId);
  const openerRef = useRef<HTMLButtonElement>(null);
  const forRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
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

  // Escape, the body scroll lock and the focus trap all live in the shared hook, so the modal and
  // the filter sheet cannot drift apart on a phone.
  useDialogShell({ open, onClose: closeModal, containerRef: dialogRef });

  useEffect(() => {
    if (!open) return;
    forRef.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
  }, [open]);

  const pickers: Array<{
    key: string;
    label: string;
    accept: string;
    icon: typeof Mic;
  }> = [
    { key: "voice", label: text("picker.voice", "Voice note"), accept: "audio/*", icon: Mic },
    {
      key: "media",
      label: text("picker.media", "Photo or video"),
      accept: "image/*,video/*",
      icon: Image,
    },
    {
      key: "file",
      label: text("picker.file", "File"),
      accept: ".pdf,.doc,.docx,.xls,.xlsx,.csv,.txt",
      icon: FileText,
    },
  ];

  return (
    <>
      <button
        ref={openerRef}
        type="button"
        className="btn p"
        onClick={openModal}
        disabled={!assignees.length}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <Plus className="ic" aria-hidden="true" />
        New task
      </button>
      {open ? (
        <>
          <button
            type="button"
            className="scrim on lt-modal-scrim"
            aria-label="Close"
            onClick={closeModal}
          />
          <div
            ref={dialogRef}
            className="lt-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby={headingId}
          >
            <div className="lt-modal-hd">
              <Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3 id={headingId}>New task</h3>
              <div className="sp" style={{ flex: 1 }} />
              <button
                type="button"
                className="btn"
                onClick={closeModal}
                aria-label="Close"
              >
                <X className="ic" aria-hidden="true" />
              </button>
            </div>
            <form
              action={action}
              className="lt-modal-bd"
              onSubmit={(event) => {
                // The person is the one required field the browser cannot check itself (a hidden
                // input is never validated), so the form checks it and says so in place.
                if (!assigneeId) {
                  event.preventDefault();
                  setAssigneeMissing(true);
                  forRef.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
                }
              }}
            >
              <input type="hidden" name="idempotency_key" value={idempotencyKey} />
              <input type="hidden" name="return_to" value={returnTo} />
              <div className="fld lt-for" ref={forRef}>
                <span className="lt-fld-label">For</span>
                <AssigneePicker
                  mode="single"
                  name="assignee_user_id"
                  labels={{
                    label: "For",
                    placeholder: "Choose who this is for",
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
                  <small id={assigneeErrorId} className="lt-fnote" role="alert">
                    {text("feedback.missing_assignee", "Choose who the task is for.")}
                  </small>
                ) : chosen ? (
                  <small className="lt-assignee-hint">
                    Assigned to <b>{chosen.name}</b>
                    {chosen.title ? <> — {chosen.title}</> : null}
                  </small>
                ) : null}
              </div>
              <label className="fld">
                <span>Title</span>
                <input
                  name="title"
                  required
                  maxLength={80}
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                />
                <small className="lt-counter">{title.length} / 80</small>
              </label>
              <label className="fld">
                <span>Brief</span>
                <textarea name="body" maxLength={4000} rows={4} />
              </label>
              <TaskDeadlineFields
                pageContract={pageContract}
                defaultLocal=""
                min={deadlineMin.slice(0, 10) || undefined}
                required
                label="Deadline"
                hint="Date and time the task is due, farm clock (IST)."
              />
              <div className="fld">
                <span className="lt-fld-label">Attachments</span>
                <div className="lt-pickers">
                  {pickers.map((picker) => {
                    const Icon = picker.icon;
                    const count = picked[picker.key] ?? 0;
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
                            setPicked((prev) => ({
                              ...prev,
                              [picker.key]: event.target.files?.length ?? 0,
                            }))
                          }
                        />
                      </label>
                    );
                  })}
                </div>
              </div>
              <button type="submit" className="btn p lt-send">
                Send
              </button>
            </form>
          </div>
        </>
      ) : null}
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
