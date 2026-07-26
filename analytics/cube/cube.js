// Mesha Cube Core configuration.
//
// Cube is the governed metric layer for the Mesha leadership assistant. It owns
// the official KPI formulas (analytics/cube/model/**) and runs SQL against
// Postgres (later BigQuery/dbt marts) through the mesha_cube_readonly role.
//
// SECURITY MODEL (mandatory):
//   - tenant_id is NEVER taken from user text. The Mesha backend signs a JWT
//     whose securityContext carries { tenant_id } from the server-side session.
//   - queryRewrite below injects a tenant_id = <session tenant> filter on every
//     cube referenced by a query, and REJECTS any query with no tenant context.
//   - The browser never calls Cube directly; only the Mesha backend does.
//
// No secret value lives in this file. CUBEJS_API_SECRET and the DB credentials
// come from the environment (.env.ceo-ai.local locally; Secret Manager in stg).

const crypto = require('crypto');

function b64urlDecodeJSON(part) {
  const padded = part + '='.repeat((4 - (part.length % 4)) % 4);
  return JSON.parse(Buffer.from(padded, 'base64url').toString('utf8'));
}

function verifySecurityContext(auth) {
  const raw = String(auth || '').replace(/^Bearer\s+/i, '').trim();
  const parts = raw.split('.');
  if (parts.length !== 3) {
    throw new Error('Cube: invalid JWT');
  }
  const [headerPart, payloadPart, sigPart] = parts;
  const header = b64urlDecodeJSON(headerPart);
  if (header.alg !== 'HS256') {
    throw new Error('Cube: unsupported JWT alg');
  }
  const expected = crypto
    .createHmac('sha256', process.env.CUBEJS_API_SECRET || '')
    .update(`${headerPart}.${payloadPart}`)
    .digest('base64url');
  const actualSig = Buffer.from(sigPart);
  const expectedSig = Buffer.from(expected);
  if (actualSig.length !== expectedSig.length || !crypto.timingSafeEqual(actualSig, expectedSig)) {
    throw new Error('Cube: invalid JWT signature');
  }
  const payload = b64urlDecodeJSON(payloadPart);
  const now = Math.floor(Date.now() / 1000);
  if (payload.exp && Number(payload.exp) < now) {
    throw new Error('Cube: expired JWT');
  }
  return payload.securityContext || payload;
}

/** Cubes/views whose tenant filter is applied by queryRewrite. Every cube in
 *  analytics/cube/model exposes a `tenant_id` dimension. */
function cubesReferenced(query) {
  const names = new Set();
  const add = (member) => {
    if (typeof member === 'string' && member.includes('.')) {
      names.add(member.split('.')[0]);
    }
  };
  (query.measures || []).forEach(add);
  (query.dimensions || []).forEach(add);
  (query.segments || []).forEach(add);
  (query.timeDimensions || []).forEach((td) => td && add(td.dimension));
  (query.filters || []).forEach(function walk(f) {
    if (!f) return;
    if (f.member) add(f.member);
    (f.and || []).forEach(walk);
    (f.or || []).forEach(walk);
  });
  return names;
}

module.exports = {
  checkAuth: (_req, auth) => ({
    security_context: verifySecurityContext(auth),
  }),

  queryRewrite: (query, { securityContext }) => {
    const tenant = securityContext && securityContext.tenant_id;
    if (!tenant) {
      throw new Error('Cube: missing tenant_id in security context');
    }
    query.filters = query.filters || [];
    cubesReferenced(query).forEach((cube) => {
      query.filters.push({
        member: `${cube}.tenant_id`,
        operator: 'equals',
        values: [String(tenant)],
      });
    });
    return query;
  },
};
