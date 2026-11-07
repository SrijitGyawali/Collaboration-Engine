import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    // Same alias as tsconfig.json, so tests can import "@/lib/...".
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    // The Go runtime keeps an idle timer alive; run each test file in a
    // child process so Vitest can always end it cleanly.
    pool: "forks",
  },
});
