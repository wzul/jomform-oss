import type { Metadata } from "next";
import { I18nProvider } from "../components/i18n";
import { SessionProvider } from "../components/session";
import "./globals.css";

export const metadata: Metadata = {
  title: "JomForm — Order forms, agent payments & sales for everyone",
  description:
    "Build beautiful order forms, accept CHIP payments, manage sales. Free forever.",
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