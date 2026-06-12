import { execFileSync, spawn } from "node:child_process";
import net from "node:net";

const mode = process.argv[2];
const host = "127.0.0.1";
const port = 3300;

if (mode !== "dev" && mode !== "start") {
  console.error("Usage: npm run dev:local|start:local");
  process.exit(2);
}

await assertPortFree(host, port);

const child = spawn("next", [mode, "-H", host, "-p", String(port)], {
  stdio: "inherit",
  env: { ...process.env, HOSTNAME: host, PORT: String(port) },
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});

async function assertPortFree(hostname, targetPort) {
  const busy = await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: hostname, port: targetPort });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", (error) => {
      socket.destroy();
      if (error.code === "ECONNREFUSED") {
        resolve(false);
        return;
      }
      reject(error);
    });
    socket.setTimeout(1000, () => {
      socket.destroy();
      resolve(false);
    });
  });

  if (!busy) return;

  console.error(`Port ${targetPort} on ${hostname} is already in use. Stop that process or choose a different terminal.`);
  try {
    const owner = execFileSync("lsof", ["-nP", `-iTCP:${targetPort}`, "-sTCP:LISTEN"], { encoding: "utf8" });
    console.error(owner.trim());
  } catch {
    console.error("Could not inspect owner with lsof.");
  }
  process.exit(1);
}
