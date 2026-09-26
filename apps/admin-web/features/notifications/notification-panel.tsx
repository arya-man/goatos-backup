"use client";

/**
 * The notification centre's body, laid out as a full-height right drawer (the bell mounts it in
 * the kit `Sheet` at every width): header "Notifications" + mark-all ✓✓ + ⚙ + close with a
 * hairline; a pill tab strip All · Unread · Archived with count chips; one row per notification;
 * infinite scroll inside the sheet; a muted push-permission row that ⚙ toggles.
 *
 * Presentational on purpose — it owns no fetching and no optimism — so the bell drives it and a
 * fixture (or a future /notifications page) can render it unchanged.
 *
 * ROW ANATOMY (16px padding, 72px floor). 40px circle on the left: the actor's initials when the
 * row has an actor, else the type's semantic icon in a tinted circle (`notificationKind`). Then
 * ONE sentence: bold actor + the backend's own headline (never composed here); a muted meta line
 * "2d ago · Verification"; the body, when there is one, as a soft quoted block (two lines, the
 * row expands it on click); short context values as outlined chips; and an inline action pair
 * ONLY where the backend gives something openable (`notificationAction`). Unread dot on the right,
 * swapping to the per-row ✓ on hover. Hairline between rows.
 *
 * Styling lives in `./notification-panel.css` (Mesha tokens only; no hexes) and the shared kit.
 */

import { motion, useReducedMotion } from "motion/react";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import { isPushCapable } from "@/lib/push-capable";
import Link from "@/components/no-prefetch-link";
import { Suspense, lazy, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import {
  AlertTriangle,
  AtSign,
  Bell,
  BellOff,
  CalendarClock,
  Check,
  CheckCheck,
  ClipboardList,
  Clock,
  HeartPulse,
  Inbox,
  MessageSquare,
  Scale,
  Settings,
  Truck,
  User,
  Wheat,
  X,
} from "lucide-react";
import { cx } from "@/lib/tone";
import { IconBadge } from "@/components/app/icon-badge";
import { Skeleton } from "@/components/app/page-skeletons";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { formatNotificationTime, type NotificationCentreCopy } from "./notification-copy";
import { notificationKind, notificationTabCounts, relativeNotificationTime, type NotificationIconName, type NotificationTab } from "./notification-kind";
import {
  isNotificationRead,
  notificationAction,
  notificationChips,
  notificationInitials,
  sortNotificationsNewestFirst,
  type InAppNotification,
  type NotificationFeed,
} from "./notification-model";
import "./notification-panel.css";

const ICONS: Record<NotificationIconName, ReactNode> = {
  bell: <Bell />,
  clock: <Clock />,
  check: <Check />,
  "at-sign": <AtSign />,
  message: <MessageSquare />,
  clipboard: <ClipboardList />,
  "heart-pulse": <HeartPulse />,
  wheat: <Wheat />,
  truck: <Truck />,
  scale: <Scale />,
  calendar: <CalendarClock />,
  alert: <AlertTriangle />,
  user: <User />,
};

/** The ⚙ section, fetched the first time it is opened (it carries `firebase/messaging`). */
const PushSettings = lazy(async () => {
  const mod = await import("./push-settings");
  return { default: mod.PushSettings };
});

const subscribeNever = () => () => {};
const readFalse = () => false;
const readBrowserCanPush = () => isPushCapable();

const SKELETON_ROWS = 5;
const STAGGER_S = 0.04;

/** Rows of avatar circle + two bars: the exact shape the list resolves into. */
export function NotificationSkeletonRows({ rows = SKELETON_ROWS }: { rows?: number }) {
  return (
    <ul className="nc-items" aria-hidden="true">
      {Array.from({ length: rows }, (_, i) => (
        <li key={i} className="nc-row nc-row-sk">
          <Skeleton width={40} height={40} radius="50%" />
          <span className="nc-main">
            <Skeleton height={14} width={`${58 + ((i * 17) % 30)}%`} />
            <Skeleton height={11} width={`${28 + ((i * 23) % 22)}%`} />
          </span>
        </li>
      ))}
    </ul>
  );
}

export function NotificationPanel({
  feed,
  centreCopy,
  busy = false,
  loading = false,
  errorCode,
  contractCopy,
  pushAvailable = false,
  onMarkRead,
  onMarkAllRead,
  onRefresh,
  onClose,
  onNavigate,
  onLoadMore,
  loadingMore = false,
}: {
  feed: NotificationFeed;
  centreCopy: NotificationCentreCopy;
  /** A mark-read write is in flight. */
  busy?: boolean;
  /** The FIRST feed read is in flight and there is nothing to show yet: shape-matched skeletons. */
  loading?: boolean;
  /** A backend error code. The sentence beside it is contract copy; nothing is composed here. */
  errorCode?: string;
  /** The bootstrap contract's copy map, for the push section's backend-owned strings. */
  contractCopy?: Record<string, string>;
  /** Server-decided: the stack can do browser push. The ⚙ section exists only when true AND this browser has the APIs. */
  pushAvailable?: boolean;
  onMarkRead: (notificationRequestId: string) => void;
  onMarkAllRead: () => void;
  /** Kept for the error row's retry; the header has no refresh button (the sheet reloads on open). */
  onRefresh: () => void;
  onClose: () => void;
  onNavigate?: (item: InAppNotification) => void;
  /** Fetches the next page (`feed.next_cursor`); absent or no cursor = no "load more" at the end. */
  onLoadMore?: () => void;
  loadingMore?: boolean;
}) {
  const reduce = useReducedMotion();
  const [tab, setTab] = useState<NotificationTab>("all");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [pushOpen, setPushOpen] = useState(false);
  // Browser capability, read once on the client (no prompt, no side effect). The server said the
  // stack can push; the browser must also have the Notification + service-worker APIs.
  const browserCanPush = useSyncExternalStore(subscribeNever, readBrowserCanPush, readFalse);
  const showPush = pushAvailable && browserCanPush;

  const all = sortNotificationsNewestFirst(feed.items);
  const counts = notificationTabCounts(all, feed.unread_count, feed.total_count);
  const rows = tab === "unread" ? all.filter((item) => !isNotificationRead(item)) : tab === "archived" ? all.filter(isNotificationRead) : all;
  const hasUnread = counts.unread > 0;
  const showSkeleton = loading && all.length === 0;
  const hasMore = Boolean(feed.next_cursor) && typeof onLoadMore === "function";
  const sentinel = useRef<HTMLDivElement | null>(null);
  const loadMoreRef = useRef(onLoadMore);
  useEffect(() => {
    loadMoreRef.current = onLoadMore;
  }, [onLoadMore]);
  // Infinite scroll: when the end-of-list sentinel enters the scroll box, ask for the next page.
  // The button below it is the same action for keyboard readers and engines without IO.
  useEffect(() => {
    const node = sentinel.current;
    if (!node || !hasMore || loadingMore) return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) loadMoreRef.current?.();
    }, { root: node.closest(".nc-scroll"), rootMargin: "120px" });
    io.observe(node);
    return () => io.disconnect();
  }, [hasMore, loadingMore, rows.length]);

  // Header actions: template notifications-drawer IconButtons (Tooltip + IconButton), so the
  // PhoneTapStyles 44px floor applies on phone.
  const iconButton = (label: string, icon: ReactNode, onClick: () => void, disabled = false, color: "default" | "primary" = "default", pressed?: boolean) => (
    <Tooltip title={label}>
      <span>
        <IconButton color={color} onClick={onClick} disabled={disabled} aria-label={label} aria-pressed={pressed} sx={pressed ? { bgcolor: "action.selected" } : undefined}>
          {icon}
        </IconButton>
      </span>
    </Tooltip>
  );

  const emptyIcon = !feed.available ? <BellOff /> : tab === "unread" ? <CheckCheck /> : <Inbox />;
  const emptyText = !feed.available ? centreCopy.unavailable : tab === "unread" ? centreCopy.emptyUnread : centreCopy.empty;

  return (
    <div className="nc" data-notification-panel>
      <div className="nc-head">
        <h2 className="nc-title">{centreCopy.title}</h2>
        {iconButton(centreCopy.markAllRead, <CheckCheck size={20} aria-hidden="true" />, onMarkAllRead, busy || !hasUnread, "primary")}
        {showPush ? iconButton(centreCopy.settings, <Settings size={20} aria-hidden="true" />, () => setPushOpen((v) => !v), false, "default", pushOpen) : null}
        {iconButton(centreCopy.close, <X size={20} aria-hidden="true" />, onClose)}
      </div>

      {showPush && pushOpen ? (
        <section className="nc-push-wrap" aria-label={centreCopy.pushTitle}>
          <Suspense fallback={<div className="nc-push"><span className="nc-push-hint">{centreCopy.busy}</span></div>}>
            <PushSettings centreCopy={centreCopy} contractCopy={contractCopy} />
          </Suspense>
        </section>
      ) : null}

      <div className="nc-tabs-wrap">
        <AnimatedTabs
          variant="pill"
          className="nc-tabs"
          ariaLabel={centreCopy.title}
          value={tab}
          onChange={(value) => setTab(value === "unread" ? "unread" : value === "archived" ? "archived" : "all")}
          items={[
            { value: "all", label: centreCopy.tabAll, count: counts.all ?? null },
            { value: "unread", label: centreCopy.tabUnread, count: counts.unread },
            { value: "archived", label: centreCopy.tabArchived, count: counts.archived ?? null },
          ]}
        />
      </div>

      {errorCode ? (
        <div className="nc-error" role="alert">
          <AlertTriangle aria-hidden="true" />
          <span>{centreCopy.error}</span>
          <button type="button" className="nc-error-retry" onClick={onRefresh} disabled={loading}>
            {centreCopy.refresh}
          </button>
        </div>
      ) : null}

      <div className="nc-scroll" aria-busy={busy || loading}>
        {showSkeleton ? (
          <NotificationSkeletonRows />
        ) : rows.length === 0 ? (
          <div className="nc-empty">
            <span className="nc-empty-ic">{emptyIcon}</span>
            <span>{emptyText}</span>
          </div>
        ) : (
          <ul key={tab} className="nc-items">
            {rows.map((item, index) => (
              <NotificationRow
                key={item.notification_request_id}
                item={item}
                index={index}
                reduce={Boolean(reduce)}
                centreCopy={centreCopy}
                busy={busy}
                expanded={expanded === item.notification_request_id}
                onToggle={() =>
                  setExpanded((current) => (current === item.notification_request_id ? null : item.notification_request_id))
                }
                onMarkRead={onMarkRead}
                onOpen={() => {
                  onMarkRead(item.notification_request_id);
                  onNavigate?.(item);
                  onClose();
                }}
              />
            ))}
          </ul>
        )}
        {hasMore && rows.length > 0 ? (
          <div ref={sentinel} className="nc-more">
            {loadingMore ? (
              <NotificationSkeletonRows rows={2} />
            ) : (
              <button type="button" className="nc-more-btn" onClick={onLoadMore}>
                {centreCopy.loadMore}
              </button>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function NotificationRow({
  item,
  index,
  reduce,
  centreCopy,
  busy,
  expanded,
  onToggle,
  onMarkRead,
  onOpen,
}: {
  item: InAppNotification;
  index: number;
  reduce: boolean;
  centreCopy: NotificationCentreCopy;
  busy: boolean;
  expanded: boolean;
  onToggle: () => void;
  onMarkRead: (id: string) => void;
  onOpen: () => void;
}) {
  const kind = notificationKind(item.notification_type);
  const read = isNotificationRead(item);
  const action = notificationAction(item);
  const chips = notificationChips(item);
  const initials = notificationInitials(item.actor_name);
  const absolute = formatNotificationTime(item.requested_at);
  const relative = relativeNotificationTime(item.requested_at);
  const body = (item.body ?? "").trim();
  const canExpand = body.length > 0;
  const actor = (item.actor_name ?? "").trim();

  return (
    <motion.li
      className={cx("nc-row", !read && "nc-unread", expanded && "nc-open")}
      initial={reduce ? false : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={reduce ? { duration: 0 } : { duration: 0.2, delay: Math.min(index, 12) * STAGGER_S, ease: [0.22, 1, 0.36, 1] }}
    >
      {initials ? (
        <span className="nc-av" aria-hidden="true" style={{ background: `var(--${kind.tone === "neutral" ? "paper-2" : `${kind.tone}-soft`})`, color: `var(--${kind.tone === "neutral" ? "fg-muted" : `${kind.tone}-ink`})` }}>
          {initials}
        </span>
      ) : (
        <IconBadge icon={ICONS[kind.icon]} tone={kind.tone} size="sm" shape="circle" className="nc-ic" />
      )}
      <div className="nc-main">
        {/* The sentence + meta (+ quote) is the expand toggle where there is a body to expand. */}
        {canExpand ? (
          <button type="button" className="nc-text" onClick={onToggle} aria-expanded={expanded} aria-label={expanded ? centreCopy.collapse : centreCopy.expand}>
            <Sentence actor={actor} title={item.title} />
            <Meta absolute={absolute} relative={relative} iso={item.requested_at} category={kind.category} />
            <span className="nc-quote" title={expanded ? undefined : body}>
              <span className={cx("nc-quote-text", !expanded && "nc-clamp")}>{body}</span>
            </span>
          </button>
        ) : (
          <span className="nc-text">
            <Sentence actor={actor} title={item.title} />
            <Meta absolute={absolute} relative={relative} iso={item.requested_at} category={kind.category} />
          </span>
        )}
        {chips.length > 0 ? (
          <span className="nc-chips">
            {chips.map((chip) => (
              <span key={chip} className="nc-chip">
                {chip}
              </span>
            ))}
          </span>
        ) : null}
        {action ? (
          <span className="nc-actions">
            <Link href={action.href} className="nc-btn nc-btn-contained" onClick={onOpen}>
              {action.kind === "load" ? centreCopy.openLoad : action.kind === "task" ? centreCopy.openTask : centreCopy.openTarget}
            </Link>
            {read ? null : (
              <button type="button" className="nc-btn nc-btn-outlined" onClick={() => onMarkRead(item.notification_request_id)} disabled={busy}>
                {centreCopy.markRead}
              </button>
            )}
          </span>
        ) : null}
      </div>
      <span className="nc-side">
        {read ? null : (
          <>
            <span className="nc-dot" aria-hidden="true" />
            <button type="button" className="nc-ibtn nc-ibtn-row" onClick={() => onMarkRead(item.notification_request_id)} disabled={busy} title={centreCopy.markRead} aria-label={centreCopy.markRead}>
              <Check aria-hidden="true" />
            </button>
          </>
        )}
      </span>
    </motion.li>
  );
}

function Sentence({ actor, title }: { actor: string; title: string }) {
  return (
    <span className="nc-t" title={actor ? `${actor} · ${title}` : title}>
      {actor ? <b className="nc-actor">{actor}</b> : null}
      {actor ? " " : null}
      <span className="nc-title-text">{title}</span>
    </span>
  );
}

function Meta({ absolute, relative, iso, category }: { absolute: string; relative: string; iso: string; category: string }) {
  return (
    <span className="nc-meta">
      <time dateTime={iso} title={absolute}>
        {relative || absolute}
      </time>
      <span aria-hidden="true">·</span>
      <span>{category}</span>
    </span>
  );
}
