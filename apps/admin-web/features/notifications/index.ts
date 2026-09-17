// The feature's PUBLIC entrypoint. Everything a route or the shell needs arrives through here:
// tools/agent-hooks/check-boundaries.sh refuses a deep import into a feature's internals.
export { NotificationBell } from "./notification-bell";
export { NotificationPanel } from "./notification-panel";
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
export { type MentionCandidate, type MentionSelection, type MentionValue } from "./mention-model";
