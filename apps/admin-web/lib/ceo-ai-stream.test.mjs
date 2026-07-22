import assert from "node:assert/strict";
import test from "node:test";

import { readCeoAiStream } from "./ceo-ai-stream.ts";

// This suite proves the committed client (readCeoAiStream) interoperates with
// the EXACT SSE framing the backend streaming handler emits:
//
//   event: token\ndata: {"type":"token","text":"..."}\n\n
//   event: final\ndata: {"type":"final","answer":...,"source":...}\n\n
//   event: error\ndata: {"type":"error","message":"..."}\n\n
//
// plus `: keepalive` / `: open` comments. The client ignores `event:` and
// comment lines and keys off the `type` discriminator inside each `data:`.

function sseResponse(sseText, { chunkSize = 9 } = {}) {
  const bytes = new TextEncoder().encode(sseText);
  let offset = 0;
  const stream = new ReadableStream({
    pull(controller) {
      if (offset >= bytes.length) {
        controller.close();
        return;
      }
      controller.enqueue(bytes.slice(offset, offset + chunkSize));
      offset += chunkSize;
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

function jsonResponse(obj, status = 200) {
  return new Response(JSON.stringify(obj), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function withFetch(impl, fn) {
  const original = globalThis.fetch;
  globalThis.fetch = impl;
  return (async () => {
    try {
      return await fn();
    } finally {
      globalThis.fetch = original;
    }
  })();
}

test("assembles backend token frames and resolves the final envelope", async () => {
  const sse =
    ": open\n\n" +
    'event: token\ndata: {"type":"token","text":"Hello "}\n\n' +
    ": keepalive\n\n" +
    'event: token\ndata: {"type":"token","text":"world"}\n\n' +
    'event: final\ndata: {"type":"final","answer":"Hello world","source":"counts","mode":"planned","request_id":"rid-1","conversation_id":"c1","citations":[{"surface":"counts","tier":"api"}]}\n\n';

  await withFetch(
    async () => sseResponse(sse),
    async () => {
      const tokens = [];
      let final = null;
      const result = await readCeoAiStream(
        { question: "how many goats" },
        { onToken: (t) => tokens.push(t), onFinal: (f) => (final = f) },
      );
      assert.equal(tokens.join(""), "Hello world");
      assert.ok(final);
      assert.equal(final.answer, "Hello world");
      assert.equal(final.source, "counts");
      assert.equal(final.request_id, "rid-1");
      assert.equal(final.conversation_id, "c1");
      assert.equal(final.citations?.[0].surface, "counts");
      assert.equal(result?.answer, "Hello world");
    },
  );
});

test("keepalive and open comments never surface as tokens", async () => {
  const sse =
    ": open\n\n" +
    ": keepalive\n\n" +
    'event: token\ndata: {"type":"token","text":"only answer"}\n\n' +
    'event: final\ndata: {"type":"final","answer":"only answer","source":"s","mode":"planned","request_id":"r"}\n\n';
  await withFetch(
    async () => sseResponse(sse),
    async () => {
      const tokens = [];
      await readCeoAiStream({ question: "q" }, { onToken: (t) => tokens.push(t) });
      assert.equal(tokens.join(""), "only answer");
    },
  );
});

test("backend error frame surfaces via onError", async () => {
  const sse =
    'event: token\ndata: {"type":"token","text":"partial"}\n\n' +
    'event: error\ndata: {"type":"error","message":"assistant_unavailable"}\n\n';
  await withFetch(
    async () => sseResponse(sse),
    async () => {
      let errMsg = null;
      await readCeoAiStream({ question: "q" }, { onError: (m) => (errMsg = m) });
      assert.equal(errMsg, "assistant_unavailable");
    },
  );
});

test("non-streaming JSON body (buffered fallback) yields one final", async () => {
  await withFetch(
    async () =>
      jsonResponse({
        answer: "buffered answer",
        source: "s",
        mode: "planned",
        request_id: "r",
        conversation_id: "c",
      }),
    async () => {
      const tokens = [];
      let final = null;
      await readCeoAiStream(
        { question: "q" },
        { onToken: (t) => tokens.push(t), onFinal: (f) => (final = f) },
      );
      assert.equal(final?.answer, "buffered answer");
      assert.equal(final?.conversation_id, "c");
      assert.deepEqual(tokens, ["buffered answer"]);
    },
  );
});

test("HTTP error envelope surfaces via onError with status", async () => {
  await withFetch(
    async () => jsonResponse({ error: "leadership_required" }, 403),
    async () => {
      let status = null;
      let message = null;
      const result = await readCeoAiStream(
        { question: "q" },
        {
          onError: (m, s) => {
            message = m;
            status = s;
          },
        },
      );
      assert.equal(result, null);
      assert.equal(status, 403);
      assert.equal(message, "leadership_required");
    },
  );
});

test("a stalled stream trips the idle timeout and degrades via onError", async () => {
  // A stream that connects but never sends a token or closes — the Vertex-slow
  // / hung case. The idle timeout must abort and surface a degraded error
  // instead of hanging forever.
  await withFetch(
    // Real fetch ties the AbortSignal to the body stream, so a timeout abort
    // rejects the pending reader.read(). Mirror that: error the stream when the
    // composed signal aborts. It otherwise never enqueues or closes.
    async (_url, init) => {
      const signal = init?.signal;
      const stalled = new ReadableStream({
        start(controller) {
          if (signal) {
            signal.addEventListener(
              "abort",
              () => controller.error(new DOMException("aborted", "AbortError")),
              { once: true },
            );
          }
        },
      });
      return new Response(stalled, {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      });
    },
    async () => {
      let status = null;
      let message = null;
      const result = await readCeoAiStream(
        { question: "q", idleTimeoutMs: 30 },
        {
          onError: (m, s) => {
            message = m;
            status = s;
          },
        },
      );
      assert.equal(result, null);
      assert.equal(status, 504);
      assert.equal(message, "assistant_timeout");
    },
  );
});

test("request body carries question, conversation_id and stream flag", async () => {
  let sentBody = null;
  await withFetch(
    async (_url, init) => {
      sentBody = JSON.parse(init.body);
      return jsonResponse({ answer: "ok", source: "s", mode: "planned", request_id: "r" });
    },
    async () => {
      await readCeoAiStream({ question: "how many goats", conversationId: "c9" }, {});
      assert.equal(sentBody.question, "how many goats");
      assert.equal(sentBody.conversation_id, "c9");
      assert.equal(sentBody.stream, true);
    },
  );
});
