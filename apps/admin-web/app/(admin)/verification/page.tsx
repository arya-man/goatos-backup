import { redirect } from "next/navigation";

// Compatibility only. Actions is the canonical backend-composed authority route.
export default function Page() {
  redirect("/actions");
}
