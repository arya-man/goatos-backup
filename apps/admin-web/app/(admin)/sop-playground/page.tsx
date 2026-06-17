import { readFileSync } from "node:fs";
import { join } from "node:path";
import { notFound } from "next/navigation";

function sopPlaygroundEnabled(): boolean {
  const env = process.env.GOATOS_ENV?.trim().toLowerCase();
  return (
    env === "local" ||
    env === "dev" ||
    process.env.GOATOS_ENABLE_SOP_PLAYGROUND === "true"
  );
}

export const dynamic = "force-dynamic";
export const metadata = {
  title: "Mesha SOP Builder Playground",
};

export default function SopPlaygroundPage() {
  if (!sopPlaygroundEnabled()) {
    notFound();
  }

  const html = readFileSync(join(process.cwd(), "features/sop-playground/playground.html"), "utf8");

  return (
    <iframe
      className="block h-[calc(100vh-5.5rem)] w-full rounded-md border border-[#334155] bg-[#090909]"
      sandbox="allow-scripts"
      srcDoc={html}
      title="Mesha SOP Builder Playground"
    />
  );
}
