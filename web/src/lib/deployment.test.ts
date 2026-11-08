// Card t_48a93084: the copy the dashboard prints must name an address that
// EXISTS on this deployment. These pin BOTH branches and, above all, pin the
// branch to the FLAG — the hosted product's APP_BASE_URL is its own apex, so a
// host-shaped rule would take the self-hosted branch on jomform.com (issue #77,
// card t_4ddab217).
import assert from "node:assert";
import { test } from "node:test";

import {
  HOSTED_CNAME_TARGET,
  addressLabel,
  cnameTarget,
  legalPageURL,
  normalizeOrigin,
  parseDeployment,
  workspaceAddressLabel,
  workspaceURL,
  type Deployment,
} from "./deployment.ts";

const HOSTED: Deployment = { selfHosted: false, origin: "https://jomform.com" };
const INSTALL: Deployment = { selfHosted: true, origin: "http://localhost:18099" };

test("hosted copy is unchanged", () => {
  assert.equal(workspaceURL("acme", HOSTED), "https://acme.jomform.com");
  // Exactly the sentence the picker printed before this change.
  assert.equal(workspaceAddressLabel("acme", HOSTED), "acme.jomform.com");
  assert.equal(legalPageURL("acme", "privacy", HOSTED), "https://acme.jomform.com/privacy");
  assert.equal(cnameTarget(HOSTED), HOSTED_CNAME_TARGET);
});

test("a self-hosted install names its own origin", () => {
  assert.equal(workspaceURL("acme", INSTALL), "http://localhost:18099/");
  assert.equal(workspaceAddressLabel("acme", INSTALL), "http://localhost:18099");
  assert.equal(cnameTarget(INSTALL), "localhost:18099");
  // The hosted legal routes are host-scoped and 404 without a subdomain, so the
  // link is not printed at all rather than pointing at a dead page.
  assert.equal(legalPageURL("acme", "privacy", INSTALL), "");
});

test("the FLAG decides, not the host", () => {
  // Flag OFF with an install-looking origin: still the hosted shape.
  assert.equal(
    workspaceURL("acme", { selfHosted: false, origin: "http://localhost:18099" }),
    "https://acme.jomform.com",
  );
  // Flag ON with the product's own apex as the origin: still the install origin.
  assert.equal(
    workspaceURL("acme", { selfHosted: true, origin: "https://jomform.com" }),
    "https://jomform.com/",
  );
  // And the same for the label a sentence renders.
  assert.equal(
    workspaceAddressLabel("acme", { selfHosted: true, origin: "https://jomform.com" }),
    "https://jomform.com",
  );
});

test("an unknown deployment prints nothing rather than the hosted address", () => {
  // null = /api/meta has not answered. A flash of "<slug>.jomform.com" on an
  // install that has no such host is the defect, not a cosmetic detail.
  assert.equal(workspaceURL("acme", null), "");
  assert.equal(workspaceAddressLabel("acme", null), "");
  assert.equal(legalPageURL("acme", "privacy", null), "");
});

test("a self-hosted install with no usable origin prints nothing", () => {
  for (const origin of ["", "   ", "/", "localhost:18099", "http://"]) {
    const d: Deployment = { selfHosted: true, origin: normalizeOrigin(origin) };
    assert.equal(workspaceURL("acme", d), "", `origin ${JSON.stringify(origin)}`);
    assert.equal(workspaceAddressLabel("acme", d), "");
    assert.equal(cnameTarget(d), "");
  }
});

test("normalizeOrigin keeps a real origin and trims its slashes", () => {
  assert.equal(normalizeOrigin("https://forms.example.com/"), "https://forms.example.com");
  assert.equal(normalizeOrigin("  http://localhost:18099//  "), "http://localhost:18099");
  assert.equal(normalizeOrigin(undefined), "");
  assert.equal(normalizeOrigin(42), "");
});

test("parseDeployment reads /v1/meta and defaults to hosted", () => {
  assert.deepEqual(parseDeployment({ self_hosted: true, origin: "http://localhost:18099/" }), {
    selfHosted: true,
    origin: "http://localhost:18099",
  });
  assert.deepEqual(parseDeployment({ self_hosted: false, origin: "https://jomform.com" }), {
    selfHosted: false,
    origin: "https://jomform.com",
  });
  // A malformed or missing body must not flip the dashboard into a branch that
  // prints someone else's origin: the hosted product is the safe reading.
  assert.deepEqual(parseDeployment(null), { selfHosted: false, origin: "" });
  assert.deepEqual(parseDeployment("nope"), { selfHosted: false, origin: "" });
  assert.deepEqual(parseDeployment({ self_hosted: "true" }), { selfHosted: false, origin: "" });
});

test("addressLabel renders an API url for display", () => {
  assert.equal(addressLabel("https://acme.jomform.com"), "acme.jomform.com");
  assert.equal(addressLabel("https://acme.jomform.com/"), "acme.jomform.com");
  assert.equal(addressLabel("http://localhost:18099/"), "localhost:18099");
  assert.equal(addressLabel(""), "");
  assert.equal(addressLabel(undefined), "");
});
