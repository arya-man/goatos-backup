"use client";

// React error boundary reporting caught render errors to Grafana Faro RUM
// (OBSERVABILITY_DESIGN.md §2.4). Wraps the admin shell subtree so a rendering error inside any
// page still reports to Faro even though the shell itself keeps rendering error UI.
//
// A plain class component is used (not @grafana/faro-react's FaroErrorBoundary) so this stays
// within the two approved Faro packages (@grafana/faro-web-sdk, @grafana/faro-web-tracing) and
// has no dependency on the React Router-oriented @grafana/faro-react package.
import { Component, useEffect, type ErrorInfo, type ReactNode } from "react";
import Button from "@mui/material/Button";
import Link from "@/components/no-prefetch-link";
import { StatePanel } from "@/components/app/state-panel";
import { faro } from "@grafana/faro-web-sdk";
import { adminRouteErrorReference, type NextRouteError } from "@/lib/admin-route-error";

type ObservabilityErrorBoundaryProps = {
  children: ReactNode;
  fallback?: (error: Error) => ReactNode;
};

type ObservabilityErrorBoundaryState = {
  error: Error | null;
};

function EmergencyErrorContent({
  onRetry,
  reference,
}: {
  onRetry?: () => void;
  reference?: string | null;
}) {
  return (
    <StatePanel
      page={false}
      title="Something went wrong"
      body="This screen failed to render. It has been reported."
      reference={reference ? `Ref ${reference}` : null}
      actions={
        <>
          {onRetry ? (
            <Button variant="contained" color="primary" onClick={onRetry}>
              Try again
            </Button>
          ) : null}
          <Button variant="outlined" color="inherit" component={Link} href="/">
            Go to home
          </Button>
        </>
      }
    />
  );
}

export function AdminRouteError({ error, reset }: { error: NextRouteError; reset: () => void }) {
  useEffect(() => {
    faro.api?.pushError(error, {
      context: {
        message: "Next.js admin route error boundary",
        digest: error.digest ?? "",
      },
    });
  }, [error]);

  return <EmergencyErrorContent onRetry={reset} reference={adminRouteErrorReference(error)} />;
}

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
      return <EmergencyErrorContent />;
    }
    return this.props.children;
  }
}
