export async function runBounded<T, R>(items: T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const out = new Array<R>(items.length);
  let next = 0;
  const workers = Math.max(1, Math.min(limit, items.length));
  await Promise.all(
    Array.from({ length: workers }, async () => {
      for (;;) {
        const index = next;
        next += 1;
        if (index >= items.length) return;
        // serial-await: allow bounded worker pool; parallelism is controlled by the caller's limit.
        out[index] = await fn(items[index]!);
      }
    }),
  );
  return out;
}
