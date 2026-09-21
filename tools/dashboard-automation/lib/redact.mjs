const SECRET_PATTERNS = [
  /Bearer\s+[A-Za-z0-9._~+/=-]+/gi,
  /([?&](?:token|access_token|id_token|signature|X-Goog-Signature|X-Amz-Signature)=)[^&\s]+/gi,
  /(postgres(?:ql)?:\/\/)([^:@/\s]+):([^@/\s]+)@/gi,
  /((?:api[_-]?key|anthropic[_-]?api[_-]?key|openai[_-]?api[_-]?key|password|secret|cookie)\s*[:=]\s*)[^\s"']+/gi
];

export function redactText(input) {
  let text = String(input ?? "");
  text = text.replace(SECRET_PATTERNS[0], "Bearer [REDACTED]");
  text = text.replace(SECRET_PATTERNS[1], "$1[REDACTED]");
  text = text.replace(SECRET_PATTERNS[2], "$1$2:[REDACTED]@");
  text = text.replace(SECRET_PATTERNS[3], "$1[REDACTED]");
  return text;
}

export function containsUnredactedSecret(input) {
  const text = String(input ?? "");
  return SECRET_PATTERNS.some((pattern) => {
    pattern.lastIndex = 0;
    for (const match of text.matchAll(pattern)) {
      if (!match[0].includes("[REDACTED]")) return true;
    }
    return false;
  });
}
