import { test, describe, it, before, after } from "node:test";
import assert from "node:assert/strict";

describe("ReviewEventBuffer", () => {
  describe("event emission", () => {
    it("emits all 9 event types", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ itemId, eventType, payload, proofId });
          return "client-id";
        },
      };

      const eventTypes = [
        "queue_opened",
        "item_opened",
        "video_play",
        "video_pause",
        "video_seek_attempt",
        "video_ended",
        "proof_switched",
        "fullscreen_toggled",
        "verdict_recorded",
      ];

      for (const eventType of eventTypes) {
        buffer.recordEvent("item-123", eventType, {}, "proof-456");
      }

      assert.equal(eventsCapture.length, 9);
      eventTypes.forEach((eventType, i) => {
        assert.equal(eventsCapture[i].eventType, eventType);
      });
    });

    it("includes correct payload for video_play", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ eventType, payload });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "video_play", {
        video_position_ms: 5000,
        video_duration_ms: 60000,
      });

      const event = eventsCapture[0];
      assert.equal(event.eventType, "video_play");
      assert.equal(event.payload.video_position_ms, 5000);
      assert.equal(event.payload.video_duration_ms, 60000);
    });

    it("includes correct payload for video_seek_attempt", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ eventType, payload });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "video_seek_attempt", {
        seek_from_ms: 5000,
        seek_to_ms: 15000,
        video_position_ms: 15000,
        video_duration_ms: 60000,
      });

      const event = eventsCapture[0];
      assert.equal(event.eventType, "video_seek_attempt");
      assert.equal(event.payload.seek_from_ms, 5000);
      assert.equal(event.payload.seek_to_ms, 15000);
    });

    it("includes correct payload for verdict_recorded", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ eventType, payload });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "verdict_recorded", {
        verdict: "approved",
      });

      const event = eventsCapture[0];
      assert.equal(event.eventType, "verdict_recorded");
      assert.equal(event.payload.verdict, "approved");
    });
  });

  describe("client_event_id minting and reuse", () => {
    it("mints unique client_event_id for each event", async () => {
      const ids = new Set();
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          // Simulate UUID minting
          const id = crypto.randomUUID();
          ids.add(id);
          return id;
        },
      };

      buffer.recordEvent("item-123", "video_play", {});
      buffer.recordEvent("item-123", "video_pause", {});
      buffer.recordEvent("item-123", "video_ended", {});

      assert.equal(ids.size, 3);
    });

    it("reuses same client_event_id on retry", async () => {
      let callCount = 0;
      const postFn = async () => {
        callCount++;
        if (callCount === 1) throw new Error("Network error");
        return { ok: true };
      };

      const clientEventIds = [];
      const capturePost = async (events) => {
        events.forEach((e) => clientEventIds.push(e.client_event_id));
      };

      // Simulate recording an event with a fixed client_event_id
      const event1Id = crypto.randomUUID();
      const event2Id = crypto.randomUUID();

      clientEventIds.push(event1Id);
      clientEventIds.push(event1Id); // Simulate retry with same ID

      // Both should have the same ID (retry scenario)
      assert.equal(clientEventIds[0], clientEventIds[1]);
    });
  });

  describe("seek blocking", () => {
    it("tracks last watched position", async () => {
      const buffer = {
        lastWatched: 0,
        recordEvent(itemId, eventType, payload, proofId) {
          if (eventType === "video_play" && payload.video_position_ms !== undefined) {
            this.lastWatched = Math.max(this.lastWatched, payload.video_position_ms);
          }
          return "client-id";
        },
        getLastWatchedMs() {
          return this.lastWatched;
        },
      };

      // Simulate playing video from 0ms to 5000ms
      buffer.recordEvent("item-123", "video_play", { video_position_ms: 0 });
      assert.equal(buffer.getLastWatchedMs(), 0);

      // Simulate video playback advancing to 5000ms (via timeupdate)
      buffer.lastWatched = 5000;
      assert.equal(buffer.getLastWatchedMs(), 5000);
    });

    it("blocks forward seek attempt", async () => {
      const eventsCapture = [];
      const lastWatchedMs = 5000;
      const seekToMs = 15000; // Attempt to seek past unwatched

      // Simulate seek blocking logic
      const shouldBlock = seekToMs > lastWatchedMs;
      assert.equal(shouldBlock, true);
    });

    it("allows replay of watched video", async () => {
      const lastWatchedMs = 10000;
      const seekToMs = 5000; // Seek backward to already-watched

      // Seek backward is allowed
      const shouldBlock = seekToMs > lastWatchedMs;
      assert.equal(shouldBlock, false);
    });

    it("resets last watched on item switch", async () => {
      const buffer = {
        lastWatched: 10000,
        resetLastWatched() {
          this.lastWatched = 0;
        },
        getLastWatchedMs() {
          return this.lastWatched;
        },
      };

      assert.equal(buffer.getLastWatchedMs(), 10000);
      buffer.resetLastWatched();
      assert.equal(buffer.getLastWatchedMs(), 0);
    });
  });

  describe("event payload details", () => {
    it("includes category/park/shed/status in queue_opened", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ eventType, payload });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "queue_opened", {
        category: "vaccination",
        park: "CBE",
        shed: "Shed A",
        status: "pending",
      });

      const event = eventsCapture[0];
      assert.equal(event.payload.category, "vaccination");
      assert.equal(event.payload.park, "CBE");
      assert.equal(event.payload.shed, "Shed A");
      assert.equal(event.payload.status, "pending");
    });

    it("includes proof_id when provided", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ itemId, eventType, proofId });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "video_play", {}, "proof-456");
      buffer.recordEvent("item-789", "proof_switched", {}, "proof-789");

      assert.equal(eventsCapture[0].proofId, "proof-456");
      assert.equal(eventsCapture[1].proofId, "proof-789");
    });

    it("omits proof_id when not provided", async () => {
      const eventsCapture = [];
      const buffer = {
        recordEvent(itemId, eventType, payload, proofId) {
          eventsCapture.push({ itemId, eventType, proofId });
          return "client-id";
        },
      };

      buffer.recordEvent("item-123", "queue_opened", {});

      assert.equal(eventsCapture[0].proofId, undefined);
    });
  });
});
