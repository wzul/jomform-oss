// Dokploy API helper — runs inside the dokploy container (host port 3000 was
// shadowed by a dev process; in-container loopback is reliable).
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
    data = text.slice(0, 300);
  }
  return { status: r.status, data };
}

async function main() {
  const [, , cmd, ...args] = process.argv;

  if (cmd === "create-project") {
    const r = await call("POST", "/project.create", {
      name: "JomForm",
      description: "Order-form builder — CHIP payments, MCP-native",
    });
    console.log("create-project:", JSON.stringify(r).slice(0, 500));
    return;
  }

  if (cmd === "find-project") {
    const r = await call("GET", "/project.all");
    const list = Array.isArray(r.data) ? r.data : [];
    const jf = list.find((p) => p.name.toLowerCase() === "jomform");
    console.log(
      JSON.stringify({
        found: !!jf,
        projectId: jf && jf.projectId,
        environments:
          jf &&
          jf.environments &&
          jf.environments.map((e) => ({ id: e.environmentId || e.id, name: e.name })),
      })
    );
    return;
  }
}

main().catch((e) => console.log("FATAL", e.message));