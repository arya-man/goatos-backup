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
 * ANATOMY is the template notifications drawer (layouts/components/notifications-drawer): the
 * header IconButtons (mark all read, settings), full-width MUI Tabs with Label counts, and the
 * NotificationItem row (40px avatar / icon circle on `background.neutral`-style tint, ListItemText
 * sentence + caption meta, `background.neutral` quote block, outlined Labels, small contained +
 * outlined Buttons, dashed divider, unread dot). Theme sx only: no stylesheet, no colour literal.
 */

import { m, useReducedMotion } from "motion/react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import ButtonBase from "@mui/material/ButtonBase";
import IconButton from "@mui/material/IconButton";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { isPushCapable } from "@/lib/push-capable";
import Link from "@/components/no-prefetch-link";
import { Suspense, lazy, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { NotificationRowsSkeleton } from "./notification-skeleton";
import { formatNotificationTime, type NotificationCentreCopy } from "./notification-copy";
import { notificationKind, notificationTabCounts, relativeNotificationTime, type NotificationIconName, type NotificationTab, type NotificationTone } from "./notification-kind";
import {
  isNotificationRead,
  notificationAction,
  notificationChips,
  notificationInitials,
  sortNotificationsNewestFirst,
  type InAppNotification,
  type NotificationFeed,
} from "./notification-model";

/** The type's semantic icon, from the template's registered (offline) Iconify set. */
const ICONS: Record<NotificationIconName, IconifyName> = {
  bell: "solar:bell-bing-bold",
  clock: "solar:clock-circle-bold",
  check: "solar:check-circle-bold",
  "at-sign": "solar:user-id-bold",
  message: "solar:chat-round-dots-bold",
  clipboard: "solar:bill-list-bold",
  "heart-pulse": "solar:medical-kit-bold",
  wheat: "solar:box-minimalistic-bold",
  truck: "carbon:delivery",
  scale: "solar:dumbbell-large-minimalistic-bold",
  calendar: "solar:calendar-date-bold",
  alert: "solar:danger-triangle-bold",
  user: "solar:user-rounded-bold",
};

/** The tone's palette key; `neutral` is the template's `background.neutral` circle. */
const TONE_PALETTE: Record<Exclude<NotificationTone, "neutral">, "primary" | "info" | "success" | "warning" | "error" | "secondary"> = {
  primary: "primary",
  info: "info",
  success: "success",
  warning: "warning",
  error: "error",
  violet: "secondary",
};

/** The 40px avatar / icon circle, tinted by the type's tone. */
function toneCircleSx(tone: NotificationTone) {
  return (theme: Theme) => {
    const key = tone === "neutral" ? null : TONE_PALETTE[tone];
    return {
      width: "var(--sp-5)",
      height: "var(--sp-5)",
      flex: "none",
      display: "flex",
      borderRadius: "50%",
      alignItems: "center",
      justifyContent: "center",
      typography: "subtitle2",
      ...(key
        ? { color: theme.vars.palette[key].dark, bgcolor: varAlpha(theme.vars.palette[key].mainChannel, 0.16), ...theme.applyStyles("dark", { color: theme.vars.palette[key].light }) }
        : { color: "text.secondary", bgcolor: "background.neutral" }),
    };
  };
}

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
    // Root = viewport: the drawer's Scrollbar body is the scroller now, and the implicit root
    // already clips by every scrolling ancestor, so the sentinel only intersects when it is on screen.
    }, { root: null, rootMargin: "120px" });
    io.observe(node);
    return () => io.disconnect();
  }, [hasMore, loadingMore, rows.length]);

  // Header actions: template notifications-drawer IconButtons (Tooltip + IconButton), so the
  // PhoneTapStyles 44px floor applies on phone.
  const iconButton = (label: string, icon: IconifyName, onClick: () => void, disabled = false, color: "default" | "primary" = "default", pressed?: boolean) => (
    <Tooltip title={label}>
      <span>
        <IconButton color={color} onClick={onClick} disabled={disabled} aria-label={label} aria-pressed={pressed} sx={pressed ? { bgcolor: "action.selected" } : undefined}>
          <Iconify icon={icon} aria-hidden="true" />
        </IconButton>
      </span>
    </Tooltip>
  );

  const emptyIcon: IconifyName = !feed.available ? "solar:bell-off-bold" : tab === "unread" ? "eva:done-all-fill" : "solar:inbox-bold";
  const emptyText = !feed.available ? centreCopy.unavailable : tab === "unread" ? centreCopy.emptyUnread : centreCopy.empty;
  const tabs: Array<{ value: NotificationTab; label: string; count: number | null; color: "default" | "info" | "success" }> = [
    { value: "all", label: centreCopy.tabAll, count: counts.all ?? null, color: "default" },
    { value: "unread", label: centreCopy.tabUnread, count: counts.unread, color: "info" },
    { value: "archived", label: centreCopy.tabArchived, count: counts.archived ?? null, color: "success" },
  ];

  return (
    <Box data-notification-panel sx={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
      {/* The title and the close button are the template drawer header (MinimalDrawer in the bell);
          this row carries the centre's own actions. */}
      <Box data-notification-actions sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 0.25, px: 1, py: 0.5 }}>
        {iconButton(centreCopy.markAllRead, "eva:done-all-fill", onMarkAllRead, busy || !hasUnread, "primary")}
        {showPush ? iconButton(centreCopy.settings, "solar:settings-bold-duotone", () => setPushOpen((v) => !v), false, "default", pushOpen) : null}
      </Box>

      {showPush && pushOpen ? (
        <Box
          component="section"
          aria-label={centreCopy.pushTitle}
          sx={{ px: 2.5, pt: 1.5, pb: 0.5, borderBottom: 1, borderBottomStyle: "dashed", borderColor: "divider" }}
        >
          <Suspense
            fallback={
              <Typography variant="caption" component="p" sx={{ color: "text.secondary", pb: 1.25 }}>
                {centreCopy.busy}
              </Typography>
            }
          >
            <PushSettings centreCopy={centreCopy} contractCopy={contractCopy} />
          </Suspense>
        </Box>
      ) : null}

      {/* Template notifications-drawer tabs: full width, the custom indicator, a Label count per tab
          (filled for All and the current tab, soft otherwise; Unread info, Archived success). */}
      <Tabs
        variant="fullWidth"
        value={tab}
        onChange={(_, value: NotificationTab) => setTab(value === "unread" ? "unread" : value === "archived" ? "archived" : "all")}
        indicatorColor="custom"
        aria-label={centreCopy.title}
      >
        {tabs.map((item) => (
          <Tab
            key={item.value}
            value={item.value}
            iconPosition="end"
            label={item.label}
            icon={
              item.count === null ? undefined : (
                <Label variant={item.value === "all" || item.value === tab ? "filled" : "soft"} color={item.color}>
                  {item.count}
                </Label>
              )
            }
          />
        ))}
      </Tabs>

      {errorCode ? (
        <Alert
          severity="error"
          role="alert"
          sx={{ mx: 2.5, mt: 1.5 }}
          action={
            <Button color="inherit" size="small" onClick={onRefresh} disabled={loading}>
              {centreCopy.refresh}
            </Button>
          }
        >
          {centreCopy.error}
        </Alert>
      ) : null}

      {/* The rows flow in the drawer's own Scrollbar body: no second scroller inside it. */}
      <Box aria-busy={busy || loading} sx={{ minHeight: 0 }}>
        {showSkeleton ? (
          <NotificationRowsSkeleton rows={SKELETON_ROWS} />
        ) : rows.length === 0 ? (
          <Box data-notification-empty sx={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 1.25, px: 2, py: 6, textAlign: "center" }}>
            <Box sx={toneCircleSx("neutral")}>
              <Iconify icon={emptyIcon} width={22} aria-hidden="true" />
            </Box>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {emptyText}
            </Typography>
          </Box>
        ) : (
          <Box component="ul" key={tab} sx={{ listStyle: "none", m: 0, p: 0 }}>
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
          </Box>
        )}
        {hasMore && rows.length > 0 ? (
          <Box ref={sentinel} data-notification-more>
            {loadingMore ? (
              <NotificationRowsSkeleton rows={2} />
            ) : (
              <Box sx={{ p: 1 }}>
                <Button fullWidth size="large" color="inherit" onClick={onLoadMore}>
                  {centreCopy.loadMore}
                </Button>
              </Box>
            )}
          </Box>
        ) : null}
      </Box>
    </Box>
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

  const text = (
    <>
      <Sentence actor={actor} title={item.title} unread={!read} />
      <Meta absolute={absolute} relative={relative} iso={item.requested_at} category={kind.category} />
    </>
  );

  return (
    <Box
      component={m.li}
      data-notification-row
      data-unread={read ? undefined : "true"}
      initial={reduce ? false : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={reduce ? { duration: 0 } : { duration: 0.2, delay: Math.min(index, 12) * STAGGER_S, ease: [0.22, 1, 0.36, 1] }}
      sx={(theme) => ({
        position: "relative",
        display: "flex",
        alignItems: "flex-start",
        gap: 2,
        p: 2.5,
        pr: 1.5,
        minHeight: "var(--table-row-h)",
        borderBottom: 1,
        borderBottomStyle: "dashed",
        borderColor: "divider",
        transition: theme.transitions.create("background-color", { duration: theme.transitions.duration.shorter }),
        ...(read ? {} : { bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.04) }),
        "&:hover": { bgcolor: read ? "action.hover" : varAlpha(theme.vars.palette.primary.mainChannel, 0.08) },
        "&:hover [data-row-mark], &:focus-within [data-row-mark]": { opacity: 1 },
        "&:hover [data-row-dot], &:focus-within [data-row-dot]": { opacity: 0 },
        "@media (hover: none)": { "& [data-row-mark]": { opacity: 1 }, "& [data-row-dot]": { opacity: 0 } },
      })}
    >
      <Box aria-hidden="true" sx={toneCircleSx(kind.tone)}>
        {initials ? initials : <Iconify icon={ICONS[kind.icon]} width={22} />}
      </Box>
      <Box sx={{ flex: "1 1 auto", minWidth: 0, display: "flex", flexDirection: "column", gap: 1 }}>
        {/* The sentence + meta (+ quote) is the expand toggle where there is a body to expand. */}
        {canExpand ? (
          <ButtonBase
            onClick={onToggle}
            aria-expanded={expanded}
            aria-label={expanded ? centreCopy.collapse : centreCopy.expand}
            sx={{ display: "flex", flexDirection: "column", alignItems: "stretch", gap: 0.5, width: 1, textAlign: "left", borderRadius: "var(--r-sm)", whiteSpace: "normal" }}
          >
            {text}
            <Box
              component="span"
              title={expanded ? undefined : body}
              sx={{ display: "block", mt: 0.75, p: 1.5, borderRadius: "var(--r-lg)", color: "text.secondary", bgcolor: "background.neutral", typography: "body2", overflowWrap: "anywhere" }}
            >
              <Box
                component="span"
                data-notification-quote
                sx={expanded ? { display: "block" } : { display: "-webkit-box", WebkitBoxOrient: "vertical", WebkitLineClamp: 2, overflow: "hidden" }}
              >
                {body}
              </Box>
            </Box>
          </ButtonBase>
        ) : (
          <Box component="span" sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            {text}
          </Box>
        )}
        {chips.length > 0 ? (
          <Box component="span" sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
            {chips.map((chip) => (
              <Label key={chip} variant="outlined">
                {chip}
              </Label>
            ))}
          </Box>
        ) : null}
        {action ? (
          <Box component="span" sx={{ display: "flex", gap: 1 }}>
            <Button component={Link} href={action.href} size="small" variant="contained" color="primary" onClick={onOpen}>
              {action.kind === "load" ? centreCopy.openLoad : action.kind === "task" ? centreCopy.openTask : centreCopy.openTarget}
            </Button>
            {read ? null : (
              <Button size="small" variant="outlined" color="inherit" onClick={() => onMarkRead(item.notification_request_id)} disabled={busy}>
                {centreCopy.markRead}
              </Button>
            )}
          </Box>
        ) : null}
      </Box>
      <Box sx={{ position: "relative", flex: "none", display: "grid", placeItems: "center", width: "var(--btn-h)", minHeight: "var(--btn-h)", mt: -0.5 }}>
        {read ? null : (
          <>
            <Box data-row-dot aria-hidden="true" sx={{ width: "var(--sp-1)", height: "var(--sp-1)", borderRadius: "50%", bgcolor: "info.main", transition: "opacity 120ms" }} />
            <Tooltip title={centreCopy.markRead}>
              <IconButton
                data-row-mark
                size="small"
                color="primary"
                onClick={() => onMarkRead(item.notification_request_id)}
                disabled={busy}
                aria-label={centreCopy.markRead}
                sx={{ position: "absolute", inset: 0, m: "auto", opacity: 0 }}
              >
                <Iconify icon="eva:checkmark-fill" width={18} aria-hidden="true" />
              </IconButton>
            </Tooltip>
          </>
        )}
      </Box>
    </Box>
  );
}

function Sentence({ actor, title, unread }: { actor: string; title: string; unread: boolean }) {
  return (
    <Typography component="span" variant="body2" title={actor ? `${actor} · ${title}` : title} sx={{ display: "block", color: "text.primary", overflowWrap: "anywhere" }}>
      {actor ? (
        <Box component="b" sx={{ typography: "subtitle2" }}>
          {actor}
        </Box>
      ) : null}
      {actor ? " " : null}
      <Box component="span" sx={unread ? { fontWeight: "fontWeightSemiBold" } : undefined}>
        {title}
      </Box>
    </Typography>
  );
}

function Meta({ absolute, relative, iso, category }: { absolute: string; relative: string; iso: string; category: string }) {
  return (
    <Box component="span" sx={{ display: "flex", alignItems: "center", gap: 0.5, typography: "caption", color: "text.disabled", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
      <Box component="time" dateTime={iso} title={absolute} sx={{ fontVariantNumeric: "tabular-nums" }}>
        {relative || absolute}
      </Box>
      <Box component="span" aria-hidden="true" sx={{ width: 2, height: 2, flex: "none", borderRadius: "50%", bgcolor: "currentColor" }} />
      <span>{category}</span>
    </Box>
  );
}
