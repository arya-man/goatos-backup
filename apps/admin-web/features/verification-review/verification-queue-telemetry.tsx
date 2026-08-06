"use client";

import React, { useEffect, useMemo } from "react";
import { ReviewEventBuffer } from "./review-events";
import { submitVerificationReviewEvents } from "./review-events-server";

export function VerificationQueueTelemetry({
  category,
  parkId,
  shedId,
  status,
  // Whether this principal may record a verdict. Telemetry ingest carries verifier-only authority, so
  // a leadership principal browsing the queue would post a batch the backend refuses. Emitting
  // nothing is the honest behaviour: the stream measures the person who signs the second check, and a
  // read-only viewer has no watch obligation to record.
  enabled,
  children,
}: {
  category?: string;
  parkId?: string;
  shedId?: string;
  status?: string;
  enabled: boolean;
  children: React.ReactNode;
}) {
  const eventBuffer = useMemo(
    () => new ReviewEventBuffer((events) => submitVerificationReviewEvents(events)),
    []
  );

  useEffect(() => {
    if (!enabled) return;
    // item_id is null: the queue screen has no item yet, and the backend rejects a placeholder
    // (422 invalid_item_id). Attribution rides on payload.category/park_id/shed_id instead.
    void eventBuffer.recordEvent(
      null,
      "queue_opened",
      {
        category: category || undefined,
        park_id: parkId || undefined,
        shed_id: shedId || undefined,
        status: status || undefined,
      }
    );
  }, [eventBuffer, enabled, category, parkId, shedId, status]);

  useEffect(() => {
    return () => {
      void eventBuffer.dispose();
    };
  }, [eventBuffer]);

  return <>{children}</>;
}
