import { TasksPage } from "@/features/tasks";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <TasksPage searchParams={await searchParams} />;
}
