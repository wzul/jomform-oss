// Payment create-purchase load test.
//
// Exercises the buyer submit path with payment_method=chip, which triggers
// the CHIP create-purchase API call. Point CHIP_API_BASE_URL at the mock
// (loadtest/mock-chip) so the load test exercises the full JomForm payment
// path (sale insert + CHIP purchase request + checkout URL attach) without
// hitting the live CHIP sandbox.
//
// Run:
//   k6 run -e VUS=50 -e DURATION=30s loadtest/payment.js
import http from "k6/http";
import { sleep } from "k6";
import {
  VUS, DURATION, RAMP, THRESH_P95, THRESH_ERR,
  publicSubmitURL, randomEmail, checkStatus,
} from "./common.js";

export const options = {
  scenarios: {
    payment: {
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
  const submitURL = publicSubmitURL();
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
    tags: { name: "payment_create_purchase" },
  });
  checkStatus(submit, 201, "create_purchase");

  // Buyer think time between purchases.
  sleep(1);
}
