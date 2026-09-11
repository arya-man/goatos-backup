import Link from "@/components/no-prefetch-link";

// Root not-found: Next serves this for any unmatched URL (a group-level file
// only answers notFound() calls). A typed or stale URL used to fall through to Next's default 404 -- a white
// "This page could not be found." panel with none of the shell's type, colour
// or theme. Like the route error boundary beside it, this renders where no
// AdminWebPageContract exists (there is no route to compile one for), so its
// two lines are literal on the same documented exception as error-boundary.tsx.
export default function NotFound() {
  return (
    <main className="wrap" style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: 24 }}>
      <section className="card" style={{ width: "100%", maxWidth: 520 }}>
        <div className="hd">
          <h3>This page does not exist</h3>
        </div>
        <p className="muted">The link is wrong or the screen has moved. Start again from the dashboard.</p>
        <Link href="/" className="btn primary">
          Go to the start
        </Link>
      </section>
    </main>
  );
}
