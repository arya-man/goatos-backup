import type { ReactNode } from "react";
import { Inbox, TriangleAlert } from "lucide-react";

import { toneVars, type KitTone } from "@/lib/tone";
import { IconBadge } from "@/components/app/icon-badge";
import styles from "./review-queue.module.css";

export const reviewQueueStyles = styles;

export function reviewStatusTone(status: string): KitTone {
  if (status === "rejected") return "error";
  if (status === "approved") return "success";
  if (status === "cancelled") return "neutral";
  return "warning";
}

/** Soft pill with a leading dot; pending states pulse gently. */
export function StatusChip({ status, children }: { status: string; children?: ReactNode }) {
  const t = toneVars(reviewStatusTone(status));
  const pending = reviewStatusTone(status) === "warning";
  return (
    <span className={styles.chip} style={{ background: t.soft, color: t.ink }}>
      <span className={`${styles.dot}${pending ? ` ${styles.pulse}` : ""}`} aria-hidden="true" />
      {children ?? status}
    </span>
  );
}

export function QueueEmptyState({ ok, title, text }: { ok: boolean; title: ReactNode; text?: ReactNode }) {
  return (
    <div className={styles.empty} role="status">
      <span className={styles.emptyIcon}>
        <IconBadge icon={ok ? <Inbox /> : <TriangleAlert />} tone={ok ? "primary" : "error"} size="lg" shape="circle" />
      </span>
      <p className={styles.emptyTitle}>{title}</p>
      {text ? <p className={styles.emptyText}>{text}</p> : null}
    </div>
  );
}
