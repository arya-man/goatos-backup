"use client";

import React, { useEffect, useMemo } from "react";
import { ReviewEventBuffer } from "./review-events";
import { submitVerificationReviewEvents } from "./review-events-server";

export function VerificationQueueTelemetry({
  category,
  parkId,
  shedId,
  status,
  children,
}: {
  category?: string;
  parkId?: string;
  shedId?: string;
  status?: string;
  children: React.ReactNode;
}) {
  const eventBuffer = useMemo(
    () => new ReviewEventBuffer((events) => submitVerificationReviewEvents(events)),
    []
  );

  useEffect(() => {
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
  }, [eventBuffer, category, parkId, shedId, status]);

  useEffect(() => {
    return () => {
      void eventBuffer.forceFlush();
    };
  }, [eventBuffer]);

  return <>{children}</>;
}
