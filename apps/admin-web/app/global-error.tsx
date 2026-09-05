"use client";

import "./global-error.css";

// The root error boundary REPLACES the root layout, so neither mesha-theme.css nor the
// Instrument @font-face blocks load here. Its styling lives in ./global-error.css.
export default function GlobalError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body suppressHydrationWarning>
        <main className="ge-main">
          <section className="ge-card">
            {/* Copy is deliberately the two strings this shell already shipped. This
                boundary replaces the root layout, so it has no AdminWebPageContract to
                read from, and the UI-contract guard rightly refuses new local literals
                here -- the design system is applied to the SHELL, not by adding copy. */}
            <h1 className="ge-title">Something went wrong</h1>
            <button type="button" onClick={reset} className="ge-btn">
              Try again
            </button>
          </section>
        </main>
      </body>
    </html>
  );
}
