"use client";

import { CalendarClock, FileText, Image, Mic, Plus, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import type { LeadershipTaskAssignee } from "@/lib/api/server";

/**
 * The "+ New task" entry on the web Tasks desk. Same shape as the phone's New task screen
 * (maintainer request 2026-09-11): For, Title, Brief, three attachment pickers, Send. It opens
 * as a modal from the button rather than sitting beside the list, and the modal is
 * client-local state -- no navigation, no document request, Escape / scrim / X close it and
 * focus returns to the button.
 */
export function NewTaskModal({
  assignees,
  action,
  returnTo,
}: {
  assignees: LeadershipTaskAssignee[];
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  const [open, setOpen] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState("");
  const [title, setTitle] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [deadline, setDeadline] = useState("");
  const [deadlineMin, setDeadlineMin] = useState("");
  const [picked, setPicked] = useState<Record<string, number>>({});
  const chosen = assignees.find((a) => a.user_id === assigneeId);
  const openerRef = useRef<HTMLButtonElement>(null);
  const firstFieldRef = useRef<HTMLSelectElement>(null);
  const headingId = useId();

  const openModal = useCallback(() => {
    setIdempotencyKey(`admin-web-leadership-task:${crypto.randomUUID()}`);
    setTitle("");
    setAssigneeId("");
    setDeadline("");
    setDeadlineMin(farmClockNow());
    setPicked({});
    setOpen(true);
  }, []);
  const closeModal = useCallback(() => {
    setOpen(false);
    openerRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;
    firstFieldRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeModal();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, closeModal]);

  const pickers: Array<{
    key: string;
    label: string;
    accept: string;
    icon: typeof Mic;
  }> = [
    { key: "voice", label: "Voice note", accept: "audio/*", icon: Mic },
    { key: "media", label: "Photo or video", accept: "image/*,video/*", icon: Image },
    { key: "file", label: "File", accept: ".pdf,.doc,.docx,.xls,.xlsx,.csv,.txt", icon: FileText },
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
            <form action={action} className="lt-modal-bd">
              <input type="hidden" name="idempotency_key" value={idempotencyKey} />
              <input type="hidden" name="return_to" value={returnTo} />
              <label className="fld">
                <span>For</span>
                {/* The picker lists people by what they ARE -- the title from People / HRMS
                    (maintainer request 2026-09-11); the person is revealed beneath once chosen.
                    Two people with one title stay distinct because each option is one person. */}
                <select
                  ref={firstFieldRef}
                  name="assignee_user_id"
                  required
                  value={assigneeId}
                  onChange={(event) => setAssigneeId(event.target.value)}
                >
                  <option value="" disabled>
                    Choose who this is for
                  </option>
                  {assignees.map((assignee) => (
                    <option key={assignee.user_id} value={assignee.user_id}>
                      {assignee.title || assignee.name}
                    </option>
                  ))}
                </select>
                {chosen ? (
                  <small className="lt-assignee-hint">
                    Assigned to <b>{chosen.name}</b>
                  </small>
                ) : null}
              </label>
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
              <label className="fld">
                <span>Deadline</span>
                {/* Date AND time, in the farm's clock (IST). The server action stamps the offset;
                    the backend refuses a raise without one or one already behind the raise
                    (maintainer decision 2026-09-14), so this is required here too. */}
                <input
                  type="datetime-local"
                  name="deadline_at"
                  required
                  min={deadlineMin || undefined}
                  step={60}
                  value={deadline}
                  onChange={(event) => setDeadline(event.target.value)}
                />
                <small className="lt-assignee-hint lt-deadline-hint">
                  <CalendarClock className="ic" aria-hidden="true" />
                  <span>Date and time the task is due, farm clock (IST).</span>
                </small>
              </label>
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
 * The earliest deadline the picker offers: now, on the farm's clock (Asia/Kolkata), in the
 * `YYYY-MM-DDTHH:MM` form `datetime-local` speaks. The browser may sit anywhere; the deadline is
 * always read as IST.
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
