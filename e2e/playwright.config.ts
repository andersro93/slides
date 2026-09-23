import { defineConfig, devices } from "@playwright/test";

// Against E2E_BASE_URL when set (CI: the smoke-tested container, with
// ./fixtures mounted at /decks). Otherwise Playwright starts the Go server
// itself on the built frontend (`bun run build` first) and the same
// fixtures — in production mode, like the container.
const baseURL = process.env.E2E_BASE_URL;
const port = 3217;

export default defineConfig({
  testDir: ".",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: baseURL ?? `http://127.0.0.1:${port}`,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: baseURL
    ? undefined
    : {
        command: "go run ./cmd/slides serve",
        cwd: "../apps/server",
        env: {
          PORT: String(port),
          APP_DIR: "../../dist/app",
          DECKS_DIR: "../../e2e/fixtures",
        },
        url: `http://127.0.0.1:${port}/healthz`,
        reuseExistingServer: false,
        timeout: 120_000,
      },
});
