import { NextRequest, NextResponse } from "next/server";

// Dashboard proxies /api/* to the Go API /v1/* so session cookies stay
// same-origin. /api/auth/login -> {API}/v1/auth/login etc.
const API = process.env.JOMFORM_API_URL || "http://localhost:8082";

function targetOf(req: NextRequest) {
  const url = new URL(req.url);
  const sub = url.pathname.replace(/^\/api/, "");
  return { path: API + "/v1" + sub + url.search };
}

async function proxy(req: NextRequest) {
  const { path } = targetOf(req);
  const headers = new Headers();
  const auth = req.headers.get("authorization");
  if (auth) headers.set("Authorization", auth);
  const cookie = req.headers.get("cookie");
  if (cookie) headers.set("Cookie", cookie);

  const init: RequestInit = { method: req.method, headers };
  if (req.method !== "GET" && req.method !== "HEAD") {
    init.body = await req.arrayBuffer();
    headers.set("Content-Type", req.headers.get("content-type") || "application/json");
  }

  const r = await fetch(path, init);
  const respHeaders = new Headers();
  const ct = r.headers.get("content-type");
  if (ct) respHeaders.set("Content-Type", ct);
  const setCookie = r.headers.get("set-cookie");
  if (setCookie) respHeaders.set("Set-Cookie", setCookie);

  return new NextResponse(r.body, { status: r.status, headers: respHeaders });
}

export const dynamic = "force-dynamic";

export const GET = proxy;
export const POST = proxy;
export const PATCH = proxy;
export const DELETE = proxy;