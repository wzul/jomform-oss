// Visual + interaction evidence for a sidebar/drawer change: drives a production
// build with a session cookie, screenshots both surfaces, and prints the measured
// facts (group labels, row heights, account-block href, document overflow).
// Screenshots land in /tmp/ — review them with your own eyes before handing off.
//
// Usage:
//   SESSION=<jf_session cookie value> BASE=http://127.0.0.1:3200 \
//     node scripts/nav-evidence.mjs
// This injects the cookie into a browser, so point it at a LOCAL build only.
import { chromium } from "playwright";

const BASE = process.env.BASE || "http://127.0.0.1:3200";
const SESSION = process.env.SESSION;
if (!SESSION) {
  console.error("SESSION is required");
  process.exit(2);
}
const url = new URL(BASE);
const cookie = { name: "jf_session", value: SESSION, domain: url.hostname, path: "/", httpOnly: true, sameSite: "Lax" };
const browser = await chromium.launch();

// ── mobile, 412x915 (the card's stated screenshot size) ──
const m = await browser.newContext({ viewport: { width: 412, height: 915 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
await m.addCookies([cookie]);
const mp = await m.newPage();
await mp.goto(`${BASE}/forms`, { waitUntil: "networkidle" });
// On a phone the <aside> is `hidden md:flex`, so it is in the DOM but not
// visible — wait for it ATTACHED, then for the settled aria-current.
await mp.waitForSelector("aside", { state: "attached", timeout: 60000 });
await mp.waitForFunction(() => document.querySelector("aside a[href='/forms']")?.getAttribute("aria-current") === "page", null, { timeout: 60000 });
await mp.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
// The shell re-renders when the session bundle lands (the workspace chip and
// aria-current appear), which detaches the hamburger mid-click — retry until the
// dialog is actually up. Click ONLY when it is not already open: a second click
// toggles the drawer shut, so a blind retry loop can end with no dialog at all.
let opened = false;
for (let i = 0; i < 20 && !opened; i++) {
  if ((await mp.locator('[role="dialog"]').count()) === 0) {
    await mp.getByRole("button", { name: "Open menu" }).click({ timeout: 10_000 }).catch(() => {});
  }
  opened = await mp
    .waitForSelector('[role="dialog"]', { state: "visible", timeout: 3_000 })
    .then(() => true)
    .catch(() => false);
}
if (!opened) throw new Error("the mobile drawer never opened");
await mp.waitForTimeout(1500);
if ((await mp.locator('[role="dialog"]').count()) === 0) {
  await mp.getByRole("button", { name: "Open menu" }).click();
  await mp.waitForSelector('[role="dialog"]', { state: "visible", timeout: 10_000 });
}

const mobile = await mp.evaluate(() => {
  const dlg = document.querySelector('[role="dialog"]');
  const groups = [...dlg.querySelectorAll("ul")].map((ul) => {
    const id = ul.getAttribute("aria-labelledby");
    return {
      label: document.getElementById(id)?.textContent.trim(),
      rows: [...ul.querySelectorAll("a")].map((a) => ({
        text: a.innerText.replace(/\s+/g, " ").trim(),
        href: a.getAttribute("href"),
        h: Math.round(a.getBoundingClientRect().height),
        current: a.getAttribute("aria-current"),
      })),
    };
  });
  const acc = dlg.querySelector('a[href="/profile"]');
  const email = [...acc.querySelectorAll("span")].find((s) => s.textContent.includes("@"));
  return {
    innerWidth: window.innerWidth,
    lang: document.documentElement.lang,
    jf_lang: localStorage.getItem("jf_lang"),
    navLandmarksInDialog: dlg.querySelectorAll("nav").length,
    groups,
    profileIsNavRow: !!dlg.querySelector('nav a[href="/profile"]'),
    account: {
      tag: acc.tagName,
      href: acc.getAttribute("href"),
      height: Math.round(acc.getBoundingClientRect().height),
      text: acc.innerText.replace(/\s+/g, " ").trim(),
      emailClipped: email.scrollWidth > email.clientWidth + 1,
      emailColor: getComputedStyle(email).color,
    },
    signOutHeight: Math.round([...dlg.querySelectorAll("button")].find((b) => b.innerText.includes("Sign out")).getBoundingClientRect().height),
    docScrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    rowsUnder44: groups.flatMap((g) => g.rows).filter((r) => r.h < 44).length,
  };
});
await mp.screenshot({ path: "/tmp/t1930d754-drawer-412x915.png" });
console.log("MOBILE 412x915 (drawer open, fresh storage):");
console.log(JSON.stringify(mobile, null, 1));

// the account block navigates (this is the card's headline behaviour)
await mp.locator('[role="dialog"] a[href="/profile"]').click();
await mp.waitForURL("**/profile", { timeout: 30000 });
console.log("\nclicking the account block landed on:", mp.url());
console.log("drawer closed after the click:", (await mp.locator('[role="dialog"]').count()) === 0);
await mp.screenshot({ path: "/tmp/t1930d754-profile-mobile.png" });

// ── desktop sidebar, 1280x800 ──
const d = await browser.newContext({ viewport: { width: 1280, height: 800 } });
await d.addCookies([cookie]);
const dp = await d.newPage();
await dp.goto(`${BASE}/profile`, { waitUntil: "networkidle" });
await dp.waitForSelector('aside a[href="/profile"][aria-current="page"]', { timeout: 60000 });
// The session bundle is what fills in the user's name/email and the
// super-admin Admin row — wait for it, or the snapshot below reads "? …" and
// reports a Help group that is missing Admin for a timing reason, not a code one.
await dp.waitForFunction(
  () => (document.querySelector('aside a[href="/profile"]')?.innerText || "").includes("@"),
  null,
  { timeout: 60000 },
);
const desktop = await dp.evaluate(() => {
  const aside = document.querySelector("aside");
  const acc = aside.querySelector('a[href="/profile"]');
  const nav = aside.querySelector("nav");
  return {
    asideDisplay: getComputedStyle(aside).display,
    navLandmarks: aside.querySelectorAll("nav").length,
    groups: [...aside.querySelectorAll("nav ul")].map((ul) => {
      const id = ul.getAttribute("aria-labelledby");
      return { label: document.getElementById(id)?.textContent.trim(), rows: [...ul.querySelectorAll("a")].map((a) => a.getAttribute("href")) };
    }),
    profileIsNavRow: !!aside.querySelector('nav a[href="/profile"]'),
    account: {
      tag: acc.tagName,
      href: acc.getAttribute("href"),
      ariaCurrent: acc.getAttribute("aria-current"),
      text: acc.innerText.replace(/\s+/g, " ").trim(),
      background: getComputedStyle(acc).backgroundColor,
    },
    // On /profile no NAV ROW is highlighted: the profile destination is the
    // account block itself, so the active state must show up there.
    activeNavRow: (() => {
      const a = aside.querySelector('nav a[aria-current="page"]');
      return a ? a.getAttribute("href") : null;
    })(),
    activeAccountBlock: (() => {
      const acc = aside.querySelector('a[href="/profile"][aria-current="page"]');
      if (!acc) return null;
      const cs = getComputedStyle(acc);
      return { ariaCurrent: acc.getAttribute("aria-current"), background: cs.backgroundColor };
    })(),
    navScrollOverflowPx: nav.scrollHeight - nav.clientHeight,
    signOutHeight: Math.round([...aside.querySelectorAll("button")].find((b) => b.innerText.includes("Sign out")).getBoundingClientRect().height),
  };
});
await dp.screenshot({ path: "/tmp/t1930d754-sidebar-1280x800.png" });
console.log("\nDESKTOP 1280x800 (on /profile):");
console.log(JSON.stringify(desktop, null, 1));

await browser.close();
