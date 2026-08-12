import { redirect } from "next/navigation";

// Compatibility only. Redirects STRAIGHT to the canonical route rather than through /actions: a
// two-hop chain costs a second round trip and is one stale link away from becoming a loop.
export default function Page() {
  redirect("/verify");
}
