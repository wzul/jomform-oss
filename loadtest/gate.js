// CI / on-demand load gate — regression guard for the buyer path.
//
// Runs the public-form scenario at a fixed concurrency (default 50 VUs) and
// FAILS the run if either threshold is exceeded:
//   - p95 latency > THRESH_P95 ms (default 500)
//   - error rate   > THRESH_ERR  (default 1%)
//
// These thresholds are the recorded baseline guard from issue #103. The gate
// is intentionally a single, cheap scenario so it can run in CI on every PR
// without a long soak. For a full baseline sweep use loadtest/baseline.sh.
//
// Run (CI or on-demand):
//   k6 run -e VUS=50 -e DURATION=30s -e THRESH_P95=500 -e THRESH_ERR=0.01 \
//     loadtest/gate.js
import http from "k6/http";
import { sleep } from "k6";
import {
  VUS, DURATION, RAMP, THRESH_P95, THRESH_ERR,
  publicFormURL, publicSubmitURL, randomEmail, checkStatus,
} from "./common.js";

export const options = {
  scenarios: {
    gate: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: RAMP, target: VUS },
        { duration: DURATION, target: VUS },
        { duration: "10s", target: 0 },
      ],
      gracefulRampDown: "10s",
    },
  },
  // The gate: fail the run if p95 latency or error rate exceeds the
  // recorded baseline threshold. k6 exits non-zero when a threshold is
  // breached, which is what CI uses to block the PR.
  thresholds: {
    http_req_failed: ["rate<" + THRESH_ERR],
    http_req_duration: ["p(95)<" + THRESH_P95],
  },
};

export default function () {
  const formURL = publicFormURL();
  const submitURL = publicSubmitURL();

  const render = http.get(formURL, { tags: { name: "gate_render" } });
  checkStatus(render, 200, "render");

  const payload = JSON.stringify({
    client: {
      full_name: "Load Test Buyer",
      email: randomEmail(),
      phone: "0123456789",
    },
    items: [{ product_id: "prod-load", quantity: 1 }],
    payment_method: "chip",
  });
  const submit = http.post(submitURL, payload, {
    headers: { "Content-Type": "application/json" },
    tags: { name: "gate_submit" },
  });
  checkStatus(submit, 201, "submit");

  sleep(1);
}
