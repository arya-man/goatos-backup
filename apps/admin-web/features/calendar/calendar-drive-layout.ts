/**
 * Drive detail roster layout, shared by the page and its loading.tsx so the skeleton cannot drift:
 * the roster's column copy keys, in order, and the rows fetched per page.
 */
export const DRIVE_ROSTER_HEADER_KEYS = [
  "calendar.drive.display_id_header",
  "calendar.drive.shed_header",
  "calendar.drive.tag_1_header",
  "calendar.drive.tag_2_header",
  "calendar.drive.stage_header",
  "calendar.drive.lifecycle_header",
  "calendar.drive.health_header",
  "calendar.drive.reason_header",
  "calendar.drive.status_header",
] as const;

export const DRIVE_ROSTER_PAGE_SIZE = 25;
