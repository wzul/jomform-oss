import type { Metadata, Viewport } from "next";
import { I18nProvider } from "../components/i18n";
import { SessionProvider } from "../components/session";
import "./globals.css";

export const metadata: Metadata = {
  title: "JomForm — Order forms, agent payments & sales for everyone",
  description:
    "Build beautiful order forms, accept CHIP payments, manage sales. Free forever.",
};

// The mobile layout depends on this: every dashboard page is laid out against
// the device width. Next injects a device-width default even without this
// export, but declaring it pins the behaviour so a Next upgrade (or a stray
// `themeColor`-style metadata change) cannot silently drop it —
// adminResponsive.test.ts asserts it is still here.
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className="antialiased">
        <I18nProvider>
          <SessionProvider>{children}</SessionProvider>
        </I18nProvider>
      </body>
    </html>
  );
}