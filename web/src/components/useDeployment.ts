"use client";

// useDeployment — the dashboard's one reader of GET /v1/meta (card t_48a93084).
//
// Every surface that prints a workspace address needs to know WHICH deployment
// it is on: the hosted product publishes <slug>.jomform.com, a self-hosted
// install publishes its own origin. The value is per-DEPLOYMENT, not
// per-workspace, so it is fetched once per page load and shared by every
// consumer on it.
//
// The hook returns null until the answer arrives. That is deliberate: the call
// sites print NOTHING rather than the hosted address while the deployment is
// unknown, because a flash of "<slug>.jomform.com" on an install that has no
// such host is the defect this card fixes, not a cosmetic detail.
import { useEffect, useState } from "react";

import { parseDeployment, type Deployment } from "../lib/deployment";

let cache: Deployment | null = null;
let inflight: Promise<Deployment> | null = null;

/**
 * loadDeployment resolves the deployment once per page load. A failure is not
 * cached: the next mount retries, so a transient error does not leave the whole
 * session on the unknown (null) branch.
 */
export function loadDeployment(): Promise<Deployment> {
  if (cache) return Promise.resolve(cache);
  if (!inflight) {
    inflight = fetch("/api/meta", { cache: "no-store" })
      .then((r) => (r.ok ? r.json() : null))
      .then((body) => {
        const d = parseDeployment(body);
        cache = d;
        return d;
      })
      .catch(() => {
        inflight = null;
        throw new Error("deployment meta unavailable");
      });
  }
  return inflight;
}

/** The current deployment, or null until /api/meta answers. */
export function useDeployment(): Deployment | null {
  const [deployment, setDeployment] = useState<Deployment | null>(cache);

  useEffect(() => {
    let alive = true;
    loadDeployment()
      .then((d) => {
        if (alive) setDeployment(d);
      })
      .catch(() => {
        // Stay on null: the copy is hidden rather than wrong.
      });
    return () => {
      alive = false;
    };
  }, []);

  return deployment;
}
