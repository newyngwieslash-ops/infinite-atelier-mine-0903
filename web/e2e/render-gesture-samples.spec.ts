import { expect, test, type Page } from "@playwright/test";

/**
 * render-gesture-samples.spec.ts is RP-11.3's gesture sampler: the plan's
 * 「1000 节点 ≥10 次手势」sample at the audit scale. It builds the T27 audit
 * scene (1000 nodes through the canvas's REAL toolbar path), then drives N
 * (GESTURES env var, default 10) real wheel gestures, recording each
 * full-rerender measurement, and prints P95/median for the results document.
 *
 * The helpers below are deliberately IDENTICAL to render-perf-t27.spec.ts's
 * (the canvas's real toolbar path, its IndexedDB clearing, its URL-derived
 * project id) — re-implementing them differently was why a first draft
 * timed out. If T27's helpers change, this file must follow.
 */

const GESTURES = Math.max(1, Number(process.env.GESTURES || "10"));
const LARGE_NODES = 1000;

async function openFreshCanvas(page: Page): Promise<string> {
    await page.goto("/canvas");
    await page.evaluate(async () => {
        indexedDB.deleteDatabase("infinite-canvas");
        window.localStorage.clear();
    });
    await page.reload();
    await page.getByRole("button", { name: /新建画布/ }).first().click();
    await expect(page).toHaveURL(/\/canvas\/.+/);
    await expect(page.locator("[data-tool='tool-text']")).toBeVisible();
    const match = page.url().match(/\/canvas\/([^/?#]+)/);
    if (!match) throw new Error("no project id in canvas URL");
    return match[1];
}

async function addNodes(page: Page, count: number): Promise<void> {
    await page.locator("[data-tool='tool-text']").first().click();
    await page.evaluate((n) => {
        return new Promise<void>((resolve) => {
            const tool = document.querySelector("[data-tool='tool-text']") as HTMLElement | null;
            if (!tool) throw new Error("toolbar button vanished");
            let done = 1;
            const step = () => {
                tool.dispatchEvent(new MouseEvent("click", { bubbles: true }));
                done++;
                if (done >= n) {
                    resolve();
                } else if (done % 100 === 0) {
                    setTimeout(step, 0);
                } else {
                    step();
                }
            };
            step();
        });
    }, count);
    await expect
        .poll(async () => page.locator("[data-node-id]").count(), { timeout: 120_000 })
        .toBe(count);
}

test("audit-scale gesture sampling (N>=10)", async ({ page }) => {
    test.setTimeout(600_000);
    await openFreshCanvas(page);
    await addNodes(page, LARGE_NODES);

    const samples: number[] = [];
    for (let i = 0; i < GESTURES; i++) {
        const renderMS = await page.evaluate(() => {
            const surface = document.querySelector(
                "main .relative.h-full.w-full.select-none.overflow-hidden",
            ) as HTMLElement | null;
            if (!surface) throw new Error("no canvas surface");
            const started = performance.now();
            surface.dispatchEvent(new WheelEvent("wheel", {
                bubbles: true, deltaY: -120,
                clientX: 400, clientY: 300,
            }));
            return new Promise<number>((resolve) => {
                requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - started)));
            });
        });
        samples.push(renderMS);
    }

    const sorted = [...samples].sort((a, b) => a - b);
    const p95Index = Math.ceil(0.95 * sorted.length) - 1;
    const p95 = sorted[p95Index];
    const median = sorted[Math.floor(sorted.length / 2)];
    console.log(`[RP11.3-gestures] gestures=${GESTURES} nodes=${LARGE_NODES} median=${median.toFixed(1)}ms p95=${p95.toFixed(1)}ms raw=[${samples.map((s) => s.toFixed(1)).join(",")}]`);
    expect(p95, "audit-scale gesture P95 within the T27 guard").toBeLessThan(2000);
});
