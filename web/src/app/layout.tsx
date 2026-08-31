import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "JomForm — Borang jualan & sistem ejen anda",
  description:
    "Bina borang tempahan yang cantik dan pantas, terima bayaran CHIP, urus jualan. Open source.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="ms">
      <body className="min-h-screen antialiased">{children}</body>
    </html>
  );
}