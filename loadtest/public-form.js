// Public form load test — the buyer path (highest traffic).
//
// Exercises:
//   1. GET  /{slug}            — SSR form render (space subdomain)
//   2. POST /{slug}/order      — buyer submit (creates a pending sale)
//
// The submit path is the heaviest buyer-facing operation: it resolves the
// published form, decrements stock, inserts a sale, and (for chip method)
// calls the CHIP create-purchase API. Point CHIP_API_BASE_URL at the mock
// (loadtest/mock-chip) so the load test does not hit the live sandbox.
//
// Run:
//   k6 run -e VUS=50 -e DURATION=30s loadtest/public-form.js
import http from "k6/http";
import { sleep } from "k6";
import { check } from "k6";
import {
  BASE_URL, VUS, DURATION, RAMP, THRESH_P95, THRESH_ERR,
  publicFormURL, publicSubmitURL, randomEmail, checkStatus,
} from "./common.js";

export const options = {
  scenarios: {
    public_form: {
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
  thresholds: {
    http_req_failed: ["rate<" + THRESH_ERR],
    http_req_duration: ["p(95)<" + THRESH_P95],
  },
};

export default function () {
  const formURL = publicFormURL();
  const submitURL = publicSubmitURL();

  // 1. Render the public form (SSR).
  const render = http.get(formURL, { tags: { name: "public_form_render" } });
  checkStatus(render, 200, "render");

  // 2. Submit an order (buyer path). Unique email per iteration.
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
    tags: { name: "public_form_submit" },
  });
  checkStatus(submit, 201, "submit");

  // Buyer think time between page views.
  sleep(1);
}
