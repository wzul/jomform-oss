"use client";

// Issue #285: social (OAuth) login post-resolution route. Password, magic-link
// and passkey logins are client-side fetches that call resolvePostLogin()
// directly after the backend responds. Social login is a FULL-PAGE HTTP
// redirect flow (browser → provider → /api/auth/callback/:provider → "/"), so
// the frontend never runs resolvePostLogin during the flow. This page is the
// landing target the backend redirects to after starting the social session:
// it runs the SAME post-login resolution (syncs jf_space, routes multi-space
// users to /space-picker, otherwise the dashboard) and drops the user where
// every other auth method drops them.
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { resolvePostLogin } from "../../lib/postLogin";

export default function PostLoginPage() {
  const router = useRouter();

  useEffect(() => {
    let cancelled = false;
    resolvePostLogin("").then((target) => {
      if (cancelled) return;
      router.replace(target);
    });
    return () => {
      cancelled = true;
    };
  }, [router]);

  // Brief neutral state while resolvePostLogin() resolves the target space.
  // Never renders Landing — anonymous visitors reach here only via the social
  // callback which has already started a session.
  return (
    <main className="flex min-h-screen items-center justify-center text-slate-400">
      …
    </main>
  );
}
