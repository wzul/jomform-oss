// Dashboard load test — authed REST endpoints + the aggregated bundle.
//
// Exercises the key dashboard round-trips:
//   1. GET /v1/rest/dashboard-bundle  — the aggregated landing bundle (issue #13/#29)
//   2. GET /v1/rest/sales             — recent sales page
//   3. GET /v1/rest/analytics/forms   — form analytics
//   4. GET /v1/rest/products          — product list
//
// Auth is via a Bearer API key (set API_KEY). The dashboard-bundle is the
// heaviest authed endpoint: it fans out to products, sales rollup, analytics
// and settings sections in one round-trip.
//
// Run:
//   k6 run -e VUS=50 -e DURATION=30s -e API_KEY=sk_... loadtest/dashboard.js
import http from "k6/http";
import { sleep } from "k6";
import {
  BASE_URL, VUS, DURATION, RAMP, THRESH_P95, THRESH_ERR,
  authHeaders, checkStatus,
} from "./common.js";

export const options = {
  scenarios: {
    dashboard: {
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
  const headers = authHeaders();

  // 1. Aggregated dashboard bundle (heaviest authed endpoint).
  const bundle = http.get(BASE_URL + "/v1/rest/dashboard-bundle", {
    headers,
    tags: { name: "dashboard_bundle" },
  });
  checkStatus(bundle, 200, "bundle");

  // 2. Recent sales page.
  const sales = http.get(BASE_URL + "/v1/rest/sales?page=1&limit=10", {
    headers,
    tags: { name: "dashboard_sales" },
  });
  checkStatus(sales, 200, "sales");

  // 3. Form analytics.
  const analytics = http.get(BASE_URL + "/v1/rest/analytics/forms", {
    headers,
    tags: { name: "dashboard_analytics" },
  });
  checkStatus(analytics, 200, "analytics");

  // 4. Product list.
  const products = http.get(BASE_URL + "/v1/rest/products", {
    headers,
    tags: { name: "dashboard_products" },
  });
  checkStatus(products, 200, "products");

  // Dashboard user think time.
  sleep(1);
}
