// Chat attachments.
//   ASK_MESHA_UPLOADS_BUCKET set -> GCS object uploads/<chatId>/<fileId>-<safeName>, plus a
//     per-request temp copy under os.tmpdir()/ask-mesha/<chatId>/ for the agent's Read tool
//     (removed by cleanup() when the request ends).
//   unset -> <stateDir>/uploads/<chatId>/<fileId>-<safeName> (local dev, original behaviour).
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";

export const safeName = (name) => name.replace(/[^\w.\- ]+/g, "_").slice(-120) || "file";

export async function createUploads({ stateDir, storage } = {}) {
  const bucketName = process.env.ASK_MESHA_UPLOADS_BUCKET;
  const LOCAL = path.join(stateDir, "uploads");

  let bucket = null;
  if (bucketName) {
    // `storage` is injectable for tests; production uses ADC (Cloud Run service account).
    if (!storage) {
      const { Storage } = await import("@google-cloud/storage");
      storage = new Storage();
    }
    bucket = storage.bucket(bucketName);
  }
  const objectName = (chatId, id, name) => `uploads/${chatId}/${id}-${safeName(name)}`;

  return {
    kind: bucket ? "gcs" : "local",
    // Returns [{ id, path, name, type }] and a cleanup() for per-request temp files.
    async save(chatId, raw) {
      const files = [];
      if (!Array.isArray(raw)) return { files, cleanup: async () => {} };
      const dir = bucket ? path.join(os.tmpdir(), "ask-mesha", chatId, crypto.randomUUID()) : path.join(LOCAL, chatId);
      for (const a of raw.slice(0, 5)) {
        if (!a || typeof a.name !== "string" || typeof a.data !== "string") continue;
        fs.mkdirSync(dir, { recursive: true });
        const id = `${Date.now()}-${crypto.randomBytes(4).toString("hex")}`;
        const type = typeof a.type === "string" ? a.type : "";
        const buf = Buffer.from(a.data, "base64");
        const file = path.join(dir, `${id}-${safeName(a.name)}`);
        fs.writeFileSync(file, buf);
        if (bucket) {
          await bucket.file(objectName(chatId, id, a.name)).save(buf, {
            resumable: false,
            contentType: type || "application/octet-stream",
          });
        }
        files.push({ id, path: file, name: a.name, type });
      }
      const cleanup = async () => {
        if (bucket) fs.rmSync(dir, { recursive: true, force: true });
      };
      return { files, cleanup };
    },
    // Readable stream for a stored file ref ({ id, name }) or null if missing.
    async open(chatId, ref) {
      if (bucket) {
        const f = bucket.file(objectName(chatId, ref.id, ref.name));
        const [exists] = await f.exists();
        return exists ? f.createReadStream() : null;
      }
      const dir = path.join(LOCAL, chatId);
      const name = fs.existsSync(dir) ? fs.readdirSync(dir).find((n) => n.startsWith(`${ref.id}-`)) : undefined;
      return name ? fs.createReadStream(path.join(dir, name)) : null;
    },
  };
}
