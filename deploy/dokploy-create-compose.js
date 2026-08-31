// Create the JomForm compose stack in Dokploy from the file on disk.
const fs = require("fs");

const KEY = process.env.DOK_API_KEY;
const BASE = "http://127.0.0.1:3000/api";
const h = { "x-api-key": KEY, "content-type": "application/json" };

async function call(method, path, body) {
  const r = await fetch(BASE + path, {
    method,
    headers: h,
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    data = text.slice(0, 400);
  }
  return { status: r.status, data };
}

async function main() {
  const composeFile = fs.readFileSync("/tmp/dokploy-compose.yml", "utf8");

  const create = await call("POST", "/compose.create", {
    name: "jomform",
    appName: "jomform-prod",
    composeFile,
    sourceType: "raw",
    composeType: "docker-compose",
    environmentId: "93pUXIRqftUc3rXZ2BcVn",
    description: "JomForm production — api/web/pg18/valkey/minio",
  });
  console.log("create:", JSON.stringify(create).slice(0, 400));
  if (create.status >= 400) return;

  const composeId = create.data && (create.data.composeId || create.data.id);
  console.log("COMPOSE_ID=" + composeId);
}

main().catch((e) => console.log("FATAL", e.message));