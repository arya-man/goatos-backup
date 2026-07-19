import assert from "node:assert/strict";
import test from "node:test";

import { adminRouteErrorReference } from "./admin-route-error.ts";

test("adminRouteErrorReference returns a bounded opaque Next.js digest", () => {
  const error = Object.assign(new Error("private backend detail"), { digest: "route_error-42.ab" });

  assert.equal(adminRouteErrorReference(error), "route_error-42.ab");
});

test("adminRouteErrorReference never exposes messages or unsafe digest text", () => {
  assert.equal(adminRouteErrorReference(new Error("database password leaked")), null);
  assert.equal(
    adminRouteErrorReference(
      Object.assign(new Error("private backend detail"), { digest: "<script>alert(1)</script>" }),
    ),
    null,
  );
  assert.equal(
    adminRouteErrorReference(
      Object.assign(new Error("private backend detail"), { digest: "x".repeat(81) }),
    ),
    null,
  );
});
