import { defineConfig } from "vite";

// The build output lands inside the Go package that go:embed reads, so the
// static assets ship inside the single binary. `outDir` is relative to the
// project root (web/). `emptyOutDir` keeps a stale build from leaving orphaned
// hashed files behind (which would trip the CI drift check).
export default defineConfig({
  build: {
    outDir: "../internal/console/static/dist",
    emptyOutDir: true,
    target: "es2020",
    sourcemap: false,
    assetsDir: "assets",
  },
});
