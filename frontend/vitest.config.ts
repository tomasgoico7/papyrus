import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    tsconfigPaths: true,
  },
  test: {
    environment: "node",
    include: ["**/*.test.ts"],
    // lib/env.ts validates these at import time, so the suite needs them present.
    env: {
      NEXT_PUBLIC_SUPABASE_URL: "https://example.supabase.co",
      NEXT_PUBLIC_SUPABASE_ANON_KEY: "test-anon-key",
      NEXT_PUBLIC_GATEWAY_URL: "http://localhost:8080",
    },
  },
});
