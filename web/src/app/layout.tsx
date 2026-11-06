import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Collab Engine",
  description: "A real-time collaborative text editor built on a CRDT written in Go.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
