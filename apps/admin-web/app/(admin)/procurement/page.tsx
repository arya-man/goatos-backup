import { redirect } from "next/navigation";

// /procurement lands on the Source Entry Board — the procurement vertical's home surface.
export default function Page() {
  redirect("/procurement/source-entry");
}
