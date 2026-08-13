/**
 * React identity for the enrollment form. The form owns a selected park in local state, so a
 * server-navigation scope change must remount it instead of carrying a park from the old scope.
 */
export function experimentEnrollerScopeKey(parks: readonly { id: string }[]): string {
  return parks.map((park) => park.id).join("|");
}
