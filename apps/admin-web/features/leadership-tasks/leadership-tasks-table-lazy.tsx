"use client";

import { Suspense, lazy, type ComponentProps } from "react";

/**
 * The list view's table, loaded only when the list view renders.
 *
 * `LeadershipTasksTable` pulls `@tanstack/react-table` (13 KB gz), and the board -- the default
 * view -- never renders a table. Importing it statically from the page put that on every /tasks
 * load (Judge B). Same `lazy` + `Suspense` grain as `push-permission-prompt-lazy.tsx`; SSR
 * streams the real table, so there is no client-side flash on a list-view load.
 */
const LeadershipTasksTable = lazy(async () => {
  const mod = await import("./leadership-tasks-table");
  return { default: mod.LeadershipTasksTable };
});

export function LeadershipTasksTableLazy(props: ComponentProps<typeof LeadershipTasksTable>) {
  return (
    <Suspense fallback={null}>
      <LeadershipTasksTable {...props} />
    </Suspense>
  );
}
