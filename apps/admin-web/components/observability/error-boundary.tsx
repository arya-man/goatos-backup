"use client";

// React error boundary reporting caught render errors to Grafana Faro RUM
// (OBSERVABILITY_DESIGN.md §2.4). Wraps the admin shell subtree so a rendering error inside any
// page still reports to Faro even though the shell itself keeps rendering error UI.
//
// A plain class component is used (not @grafana/faro-react's FaroErrorBoundary) so this stays
// within the two approved Faro packages (@grafana/faro-web-sdk, @grafana/faro-web-tracing) and
// has no dependency on the React Router-oriented @grafana/faro-react package.
import { Component, type ErrorInfo, type ReactNode } from "react";
import { faro } from "@grafana/faro-web-sdk";

type ObservabilityErrorBoundaryProps = {
  children: ReactNode;
  fallback?: (error: Error) => ReactNode;
};

type ObservabilityErrorBoundaryState = {
  error: Error | null;
};

export class ObservabilityErrorBoundary extends Component<
  ObservabilityErrorBoundaryProps,
  ObservabilityErrorBoundaryState
> {
  state: ObservabilityErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ObservabilityErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    // faro.api is undefined when Faro was never initialized (no collector configured, or SSR) —
    // reporting is then a safe no-op and the error still surfaces via the fallback UI below.
    faro.api?.pushError(error, {
      context: {
        message: "React error boundary",
        componentStack: errorInfo.componentStack ?? "",
      },
    });
  }

  render(): ReactNode {
    const { error } = this.state;
    if (error) {
      if (this.props.fallback) {
        return this.props.fallback(error);
      }
      return (
        <main className="wrap" style={{ padding: 24 }}>
          <section className="card">
            <h1>Something went wrong</h1>
            <p className="muted">
              This screen failed to render. The error has been reported. Reload the page to try again.
            </p>
          </section>
        </main>
      );
    }
    return this.props.children;
  }
}
