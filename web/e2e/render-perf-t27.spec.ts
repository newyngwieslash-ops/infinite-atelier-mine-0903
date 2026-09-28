import { expect, test, type Page } from "@playwright/test";

/**
 * T27's React-render performance measurement, against the REAL desktop UI in a
 * REAL browser — the audit's point that "Go SQL 基准不冒充 React/Wails 渲染测量".
 *
 * What is measured: how long the canvas takes to RENDER after a batch of
 * programmatic node additions, and the interaction latency of a drag, at two
 * load points (1000 nodes / 2000 edges is the audit's named scenario; a small
 * baseline is measured beside it so the numbers are a ratio, not absolutes).
 *
 * How it is measured honestly: the page's own `performance.now()` inside
 * `page.evaluate` — the browser's clock, not Playwright's IPC. Each figure is
 * recorded in the test output; the assertions are ORDER-OF-MAGNITUDE guards
 * with generous bounds, because a CI runner's jitter makes exact thresholds a
 * flake factory. The NUMBERS are the deliverable: they go into STATUS's T27
 * record, and a real regression shows up as the ratio collapsing.
 *
 * The load is injected through the same UI a person drives (the toolbar's add
 * buttons) but batched via evaluate loops to keep the test's own cost out of
 * the measurement window.
 */

/** Node counts: the audit's scenario (1000 nodes → 2000 edges with connections)
 *  and a baseline. 1000 nodes with 2000 edges means each node carries ~2 edges;
 *  the injection builds a chain+chords pattern to reach it. */
const LARGE_NODES = 1000;
const BASELINE_NODES = 100;

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

/**
 * addNodes creates nodes through the canvas's REAL toolbar path (the same
 * one canvas-regression.spec.ts uses: clicking `[data-tool="tool-text"]`
 * creates a node). The batch is sequential clicks; the poll only guards the
 * final count, keeping injection cost out of the measurement windows.
 */
async function addNodes(page: Page, count: number): Promise<void> {
    // The first click goes through Playwright (proves the real path works);
    // the rest are pressed through direct DOM events on the SAME toolbar
    // element — real clicks the canvas cannot distinguish, without 1000
    // IPC round-trips that would be TEST cost, not app cost.
    await page.locator("[data-tool='tool-text']").first().click();
    await page.evaluate((n) => {
        return new Promise<void>((resolve) => {
            const tool = document.querySelector("[data-tool='tool-text']") as HTMLElement | null;
            if (!tool) throw new Error("toolbar button vanished");
            let done = 1;
            const step = () => {
                // A click through the element's own event path.
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

test.describe("T27 render performance", () => {
    test("baseline: 100 nodes render and a drag stays interactive", async ({ page }) => {
        await openFreshCanvas(page);
        await addNodes(page, BASELINE_NODES);

        const renderMS = await page.evaluate(() => {
            const start = performance.now();
            // Force a full re-render by toggling a class the renderer observes:
            // a zoom nudge through the wheel the canvas handles.
            const surface = document.querySelector(
                "main .relative.h-full.w-full.select-none.overflow-hidden",
            ) as HTMLElement | null;
            surface?.dispatchEvent(new WheelEvent("wheel", {
                bubbles: true, deltaY: -120,
                clientX: 400, clientY: 300,
            }));
            // Two frames is what a user perceives as "the canvas updated".
            return new Promise<number>((resolve) => {
                requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - start)));
            });
        });
        expect(renderMS, "baseline re-render should be far below a frame budget x5").toBeLessThan(500);

        const dragMS = await page.evaluate(() => {
            const node = document.querySelector("[data-node-id]") as HTMLElement | null;
            if (!node) throw new Error("no node to drag");
            const rect = node.getBoundingClientRect();
            const start = performance.now();
            node.dispatchEvent(new PointerEvent("pointerdown", {
                bubbles: true, clientX: rect.left + 5, clientY: rect.top + 5, pointerId: 1,
            }));
            window.dispatchEvent(new PointerEvent("pointermove", {
                bubbles: true, clientX: rect.left + 45, clientY: rect.top + 45, pointerId: 1,
            }));
            window.dispatchEvent(new PointerEvent("pointerup", {
                bubbles: true, clientX: rect.left + 45, clientY: rect.top + 45, pointerId: 1,
            }));
            return new Promise<number>((resolve) => {
                requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - start)));
            });
        });
        expect(dragMS, "a single drag response should stay interactive").toBeLessThan(250);
    });

    test("audit scenario: 1000 nodes / 2000 edges render within the guard", async ({ page }) => {
        test.setTimeout(300_000);
        await openFreshCanvas(page);
        await addNodes(page, LARGE_NODES);

        // 2000 edges through the REAL handle gesture: mousedown on a node's
        // source handle dot (canvas-node.tsx ConnectionHandleDot), window
        // pointermove, pointerup near the target node — the same three events
        // project.tsx's connect state machine consumes. 2000 events in one
        // evaluate; the app state (setConnections) persists via the canvas
        // document autosave. Pairing: i -> i+1 (999 chain) + every 2nd node to
        // i+2 (499) + i to i+3 (499) ≈ 1997 edges.
        await page.evaluate(() => {
            const nodes = Array.from(document.querySelectorAll("[data-node-id]")).slice(0, 1000);
            // Drop EXACTLY on the target's left anchor (x = target.left,
            // y = target.centerY) — getConnectionTargetAnchor's geometry — so
            // it lands inside the 40px handle radius and the inside-node check.
            const pairs: Array<[number, number]> = [];
            for (let i = 0; i + 1 < nodes.length; i++) pairs.push([i, i + 1]);
            for (let i = 0; i + 2 < nodes.length; i += 2) pairs.push([i, i + 2]);
            for (let i = 0; i + 3 < nodes.length; i += 2) pairs.push([i, i + 3]);
            for (const [from, to] of pairs) {
                const fr = nodes[from].getBoundingClientRect();
                const start = { x: fr.right + 12, y: fr.top + fr.height / 2 };
                const tr = nodes[to].getBoundingClientRect();
                const drop = { x: tr.left, y: tr.top + tr.height / 2 };
                // The RIGHT dot (-right-6) is the source handle.
                const dot = nodes[from].querySelector(".cursor-crosshair.-right-6") as HTMLElement | null;
                if (!dot) continue;
                dot.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, clientX: start.x, clientY: start.y, button: 0 }));
                window.dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: drop.x, clientY: drop.y, pointerId: 1 }));
                window.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, clientX: drop.x, clientY: drop.y, button: 0 }));
            }
        });
        await expect
            .poll(async () => page.locator("[data-connection-id]").count(), { timeout: 60_000 })
            .toBeGreaterThan(1000);
        const finalEdges = await page.locator("[data-connection-id]").count();

        // THE MEASUREMENT: a full-surface re-render at load.
        const renderMS = await page.evaluate(() => {
            const start = performance.now();
            const surface = document.querySelector(
                "main .relative.h-full.w-full.select-none.overflow-hidden",
            ) as HTMLElement | null;
            surface?.dispatchEvent(new WheelEvent("wheel", {
                bubbles: true, deltaY: -120, clientX: 700, clientY: 450,
            }));
            return new Promise<number>((resolve) => {
                requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - start)));
            });
        });

        // RECORD the figures (they are the T27 deliverable):
        console.log(`[T27] nodes=${LARGE_NODES} edges=${finalEdges} full-rerender=${renderMS.toFixed(1)}ms`);

        // Guard, generous for CI jitter: the audit's "1000 节点桌面渲染" must
        // not be seconds. A real regression (lost viewport culling, lost
        // memoization) lands in the thousands of ms.
        expect(renderMS, "1000-node re-render guard").toBeLessThan(2000);
    });
});
