// The feature's PUBLIC entrypoint. Everything a route or the shell needs arrives through here:
// tools/agent-hooks/check-boundaries.sh refuses a deep import into a feature's internals.
export { NotificationBell } from "./notification-bell";
// `NotificationPanel` is DELIBERATELY NOT exported. It is a feature internal, rendered only by the
// bell, which now fetches it lazily on the first open -- and a re-export here defeats exactly
// that: the shell imports this barrel, so a static `export ... from "./notification-panel"` pulls
// the panel back into the globally shared chunk of all 63 routes even though nothing reads it from
// here. Measured: it held 5.3 KB gzip app-wide. If a future surface (a /notifications page) needs
// the panel, export it then -- and check what that does to the shared bundle.
// `MentionTextarea` is the EAGER half of the composer -- the named `<textarea>` and the hidden
// mentions input -- and is exported statically on purpose: the no-JS submit depends on the field
// being in the server-rendered HTML. Its @-picker (`./mention-picker`, with `./mention-model`
// behind it) is NOT exported here for the same reason the panel is not: the shell imports this
// barrel, so a static re-export would drag the picker back into the shared chunk of all 63 routes
// and undo the 4.6 KB gzip the on-demand fetch saves them.
export { MentionTextarea } from "./mention-textarea";
export {
  resolveMentionComposerCopy,
  resolveNotificationCentreCopy,
  type MentionComposerCopy,
  type NotificationCentreCopy,
} from "./notification-copy";
export {
  leadershipTaskNotificationHref,
  type InAppNotification,
  type NotificationFeed,
} from "./notification-model";
// Types only, and from the eager half deliberately: a value re-export from `./mention-model`
// here would pull the picker's rulebook into every route's chunk.
export { type MentionCandidate, type MentionSelection, type MentionValue } from "./mention-value.ts";
