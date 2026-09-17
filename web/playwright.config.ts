import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright configuration for the free-canvas regression.
 *
 * `docs/ACCEPTANCE.md` T6 names Playwright as the E2E layer and AC-CANVAS-004
 * lists the canvas behaviours it must cover. The suite runs against the Vite dev
 * server on loopback, and every test intercepts the network so nothing reaches a
 * provider: the canvas regression is about the canvas, not about a model.
 *
 * The desktop shell is not launched: a Wails window cannot be driven from a test
 * runner, so the suite covers the browser-mode canvas and the Go-side behaviour
 * is covered by the Go tests. What this suite must prove is that the interaction
 * the user performs still works, which is the regression PRD R3 warns about.
 */
export default defineConfig({
    testDir: "./e2e",
    // The canvas keeps session state in memory; parallel workers in one browser
    // would share localForage and interfere with each other's projects.
    fullyParallel: false,
    workers: 1,
    retries: 0,
    timeout: 60_000,
    expect: { timeout: 10_000 },
    reporter: [["list"]],
    use: {
        baseURL: "http://127.0.0.1:3100",
        trace: "retain-on-failure",
        // The canvas is a pointer-driven surface; a fixed viewport keeps element
        // coordinates stable across runs.
        viewport: { width: 1440, height: 900 },
    },
    projects: [
        {
            name: "chromium",
            use: { ...devices["Desktop Chrome"] },
        },
    ],
    webServer: {
        // A dedicated port, so the suite never disturbs a developer's own server.
        command: "npm run dev -- --port 3100 --strictPort",
        url: "http://127.0.0.1:3100",
        reuseExistingServer: false,
        timeout: 120_000,
    },
});
