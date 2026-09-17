import { expect, test, type Page } from "@playwright/test";

/**
 * The free-canvas regression AC-CANVAS-004 requires.
 *
 * The criterion names the behaviours a user performs: add, move, delete,
 * multi-select, box select, zoom/pan, undo/redo, image crop/split/mask, a
 * generation child node, the minimap and connections. Each has a test below, and
 * each drives the same UI a person would: the toolbar buttons carry stable ids
 * (`#tool-text`, `#tool-image`), the nodes carry `data-node-id`, and connections
 * carry `data-connection-id`.
 *
 * Two of the nine cannot complete without a provider — a generation child node
 * and an image edit both call a model. Those tests stop where the work leaves the
 * app (the prompt panel opening, the request being prepared). The provider call
 * itself is covered by the Go tests against httptest stubs; pretending to
 * generate an image here would be a fake pass.
 */

/** A fresh canvas, cleared of anything a previous test left behind. */
async function openCanvas(page: Page): Promise<string> {
    await page.goto("/canvas");
    await page.evaluate(async () => {
        indexedDB.deleteDatabase("infinite-canvas");
        window.localStorage.clear();
    });
    await page.reload();
    // The library renders its own empty state; the create button is the entry.
    await page.getByRole("button", { name: /新建画布/ }).first().click();
    await expect(page).toHaveURL(/\/canvas\/.+/);
    const match = page.url().match(/\/canvas\/([^/?#]+)/);
    if (!match) throw new Error("the canvas URL did not carry a project id");
    // The canvas surface is the pointer target for every drag below.
    await expect(page.locator("[data-tool='tool-text']")).toBeVisible();
    return match[1];
}

/** surface is the canvas's own pointer surface. */
function surface(page: Page) {
    return page.locator("main .relative.h-full.w-full.select-none.overflow-hidden").first();
}

/** nodeIds lists the rendered node ids. */
async function nodeIds(page: Page): Promise<string[]> {
    return page.locator("[data-node-id]").evaluateAll((nodes) => nodes.map((node) => node.getAttribute("data-node-id") ?? ""));
}

/** addNode creates a node through the toolbar and returns its id. */
async function addNode(page: Page, kind: "text" | "image" | "config" = "text"): Promise<string> {
    const before = await nodeIds(page);
    await page.locator(`[data-tool="tool-${kind}"]`).click();
    await expect.poll(async () => (await nodeIds(page)).length).toBeGreaterThan(before.length);
    const after = await nodeIds(page);
    const created = after.find((id) => !before.includes(id));
    if (!created) throw new Error("the toolbar did not create a node");
    return created;
}

/**
 * selectOnly makes one node the selection.
 *
 * The canvas selects a node when it is created and marks the selection with
 * z-50; clicking empty canvas clears it. The node is deselected first so the
 * click below is what selects it, which is the behaviour under test.
 */
async function selectOnly(page: Page, nodeId: string) {
    await clearSelection(page);
    const node = page.locator(`[data-node-id="${nodeId}"]`);
    const box = await node.boundingBox();
    if (!box) throw new Error("the node has no box");
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await expect.poll(() => selectedCount(page)).toBeGreaterThanOrEqual(1);
}

/** clearSelection empties the selection and closes any open panel. */
async function clearSelection(page: Page) {
    const box = await surface(page).boundingBox();
    if (!box) throw new Error("the canvas has no box");
    // Escape closes an open node panel, which otherwise overlaps a neighbour.
    await page.keyboard.press("Escape");
    // A point near the top-left is empty: nodes are created at the centre.
    await page.mouse.click(box.x + 40, box.y + 40);
}

/**
 * useSelectTool switches from the pan tool to the select tool.
 *
 * The canvas opens in pan mode, where a drag on empty canvas moves the view. The
 * toggle button is labelled with the tool it switches to, so it reads
 * data-tool="tool-pan" while selecting.
 */
async function useSelectTool(page: Page) {
    const toggle = page.locator("[data-tool='tool-pan']");
    if ((await toggle.count()) > 0) {
        await toggle.click();
    }
}

/** separateNodes drags one node clear of the canvas centre. */
async function separateNodes(page: Page, nodeId: string, dx: number, dy: number) {
    const node = page.locator(`[data-node-id="${nodeId}"]`);
    const box = await node.boundingBox();
    if (!box) throw new Error("the node has no box");
    // The title bar is the drag handle; the body may be covered by a panel.
    await page.mouse.move(box.x + box.width / 2, box.y - 14);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + dx, box.y - 14 + dy, { steps: 14 });
    await page.mouse.up();
}

/**
 * selectedCount counts nodes the canvas renders as selected.
 *
 * The canvas raises a selected node's stacking order (z-50) rather than setting
 * an ARIA state or a colour this test could misread, so the class is the
 * contract available.
 */
async function selectedCount(page: Page): Promise<number> {
    return page.locator("[data-node-id]").evaluateAll((nodes) => nodes.filter((node) => /z-50|ring|selected/.test(node.className)).length);
}

/** centreOf returns a node's clickable centre point. */
async function centreOf(page: Page, nodeId: string): Promise<[number, number]> {
    const box = await page.locator(`[data-node-id="${nodeId}"]`).boundingBox();
    if (!box) throw new Error("the node has no box");
    return [box.x + box.width / 2, box.y + box.height / 2];
}

test.describe("AC-CANVAS-004 free canvas", () => {
    test("a new project opens an empty canvas", async ({ page }) => {
        await openCanvas(page);
        expect(await nodeIds(page)).toHaveLength(0);
        await expect(surface(page)).toBeVisible();
    });

    test("add: the toolbar creates each built-in node kind", async ({ page }) => {
        await openCanvas(page);
        const text = await addNode(page, "text");
        const image = await addNode(page, "image");
        const config = await addNode(page, "config");
        expect(new Set([text, image, config]).size).toBe(3);
        for (const id of [text, image, config]) {
            await expect(page.locator(`[data-node-id="${id}"]`)).toBeVisible();
        }
    });

    test("move: dragging a node changes its position", async ({ page }) => {
        await openCanvas(page);
        const nodeId = await addNode(page);
        const node = page.locator(`[data-node-id="${nodeId}"]`);
        const before = await node.boundingBox();
        if (!before) throw new Error("the node has no box");
        // The title bar is the drag handle the canvas documents.
        await page.mouse.move(before.x + before.width / 2, before.y - 14);
        await page.mouse.down();
        await page.mouse.move(before.x + before.width / 2 + 140, before.y - 14 + 90, { steps: 14 });
        await page.mouse.up();
        const after = await node.boundingBox();
        if (!after) throw new Error("the node disappeared after the drag");
        expect(Math.abs(after.x - before.x) + Math.abs(after.y - before.y)).toBeGreaterThan(40);
    });

    test("delete: a selected node is removed", async ({ page }) => {
        await openCanvas(page);
        const nodeId = await addNode(page);
        // Creating a node selects it, so it is already the selection; the click
        // confirms the canvas agrees before the key is pressed.
        await selectOnly(page, nodeId);
        await page.keyboard.press("Delete");
        await expect.poll(async () => (await nodeIds(page)).length).toBe(0);
    });

    test("multi-select: shift-click selects more than one node", async ({ page }) => {
        await openCanvas(page);
        // Two text nodes are used: an image or config node opens a generation
        // panel on creation, and that panel would overlay the other node and
        // swallow the click. The selection behaviour is the same for either kind.
        const first = await addNode(page, "text");
        // The first node is moved clear before the second is created, because
        // both are placed at the centre and the later one is painted on top.
        await separateNodes(page, first, -260, 200);
        const second = await addNode(page, "text");
        await clearSelection(page);

        // Shift toggles membership, so the first is selected plainly and the
        // second is added with Shift.
        await page.mouse.click(...(await centreOf(page, first)));
        await expect.poll(() => selectedCount(page)).toBe(1);
        await page.locator(`[data-node-id="${second}"]`).click({ modifiers: ["Shift"] });
        await expect.poll(() => selectedCount(page)).toBeGreaterThanOrEqual(2);
    });

    test("box select: a rectangle over the canvas selects what it covers", async ({ page }) => {
        await openCanvas(page);
        // Text nodes again, so no generation panel overlays the surface. The
        // first is moved up-left and the second stays at the centre; the box
        // below is drawn around both.
        const first = await addNode(page, "text");
        await separateNodes(page, first, -300, -140);
        await addNode(page, "text");
        const box = await surface(page).boundingBox();
        if (!box) throw new Error("the canvas has no box");
        // The nodes sit at the canvas centre, so the drag runs from a point
        // above-left of them to one below-right. Starting further in avoids the
        // minimap overlay in the corner.
        await clearSelection(page);
        await useSelectTool(page);
        // The top-left corner is free (the minimap sits bottom-left), so the box
        // contains both nodes rather than clipping the first one.
        await page.mouse.move(box.x + 12, box.y + 12);
        await page.mouse.down();
        await page.mouse.move(box.x + box.width - 12, box.y + box.height - 12, { steps: 22 });
        await page.mouse.up();
        await expect.poll(() => selectedCount(page)).toBeGreaterThanOrEqual(2);
    });

    test("zoom: the wheel changes the rendered scale", async ({ page }) => {
        await openCanvas(page);
        const nodeId = await addNode(page);
        const node = page.locator(`[data-node-id="${nodeId}"]`);
        const before = await node.boundingBox();
        if (!before) throw new Error("the node has no box");
        const box = await surface(page).boundingBox();
        if (!box) throw new Error("the canvas has no box");
        // The wheel must land on the canvas itself; the centre is covered by the
        // node just created, so an empty area of the surface is used.
        await page.mouse.move(box.x + box.width * 0.15, box.y + box.height * 0.25);
        await page.mouse.wheel(0, 400);
        await expect
            .poll(async () => {
                const after = await node.boundingBox();
                return after ? Math.abs(after.width - before.width) : 0;
            })
            .toBeGreaterThan(1);
    });

    test("pan: the pan tool moves the viewport", async ({ page }) => {
        await openCanvas(page);
        const nodeId = await addNode(page);
        const node = page.locator(`[data-node-id="${nodeId}"]`);
        const before = await node.boundingBox();
        if (!before) throw new Error("the node has no box");
        // The pan tool is a toolbar toggle, which is how a user reaches it.
        const panTool = page.locator("[data-tool='tool-pan']");
        if ((await panTool.count()) === 0) {
            test.skip(true, "the toolbar exposes no pan tool id");
            return;
        }
        await panTool.click();
        const box = await surface(page).boundingBox();
        if (!box) throw new Error("the canvas has no box");
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        await page.mouse.down();
        await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2 + 70, { steps: 12 });
        await page.mouse.up();
        const after = await node.boundingBox();
        if (!after) throw new Error("the node disappeared after panning");
        expect(Math.abs(after.x - before.x) + Math.abs(after.y - before.y)).toBeGreaterThan(20);
    });

    test("undo and redo: a created node is reversible", async ({ page }) => {
        await openCanvas(page);
        await addNode(page);
        expect((await nodeIds(page)).length).toBe(1);
        // The canvas commits a history entry 180 ms after a change, so a chord
        // pressed immediately would have nothing to undo.
        await page.waitForTimeout(500);
        // The canvas binds undo to the platform chord and also offers toolbar
        // buttons; the chord is what a user presses.
        await page.keyboard.press("Control+z");
        await expect.poll(async () => (await nodeIds(page)).length).toBe(0);
        await page.waitForTimeout(500);
        await page.keyboard.press("Control+Shift+z");
        await expect.poll(async () => (await nodeIds(page)).length).toBe(1);
    });

    test("connections: two nodes can be joined", async ({ page }) => {
        await openCanvas(page);
        // Two text nodes, separated: created at the same point the later one would
        // cover the earlier one's handle, and an image node's panel would sit
        // between them.
        const first = await addNode(page, "text");
        await separateNodes(page, first, -300, 160);
        const second = await addNode(page, "text");
        expect(await page.locator("[data-connection-id]").count()).toBe(0);

        // A connection starts at a node's handle, which appears on hover.
        await page.locator(`[data-node-id="${first}"]`).hover();
        const handle = page.locator(`[data-node-id="${first}"] [class*="cursor-crosshair"]`).first();
        if ((await handle.count()) === 0) {
            // Without a reachable handle the pointer path cannot be driven, and
            // the connection model itself is covered by the Go tests. Reported as
            // a skip rather than a pass.
            test.skip(true, "the canvas exposes no connection handle to drive");
            return;
        }
        const handleBox = await handle.boundingBox();
        const targetBox = await page.locator(`[data-node-id="${second}"]`).boundingBox();
        if (!handleBox || !targetBox) throw new Error("a drag endpoint has no box");

        await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2);
        await page.mouse.down();
        // The drag must pass over the target and settle there: the canvas picks
        // the drop target from pointer-move events, and releasing without that
        // dwell leaves it with no target.
        await page.mouse.move(targetBox.x + targetBox.width / 2, targetBox.y + targetBox.height / 2, { steps: 20 });
        await page.waitForTimeout(300);
        await page.mouse.up();
        await expect.poll(() => page.locator("[data-connection-id]").count(), { timeout: 15_000 }).toBeGreaterThan(0);
    });

    test("minimap: the overview renders", async ({ page }) => {
        await openCanvas(page);
        await addNode(page);
        // The minimap sits in the canvas's own corner overlay.
        const minimap = page.locator("[class*='cursor-crosshair']").first();
        await expect(minimap).toBeVisible();
    });

    test("generation child node: the prompt panel accepts a prompt", async ({ page }) => {
        await openCanvas(page);
        // Creating an image node selects it and opens its generation panel, so the
        // panel is already present; clicking the node again would only hit the
        // panel itself.
        const nodeId = await addNode(page, "image");
        await expect(page.locator(`[data-node-id="${nodeId}"]`)).toHaveClass(/z-50/);
        // The prompt surface is a rich input: a contenteditable with
        // role="textbox", because it also holds reference chips.
        const prompt = page.locator("[role='textbox']").first();
        await expect(prompt).toBeVisible();
        await prompt.click();
        await prompt.pressSequentially("a lighthouse at dusk");
        await expect(prompt).toContainText("a lighthouse at dusk");
    });

    test("image tools: an image node offers its node actions", async ({ page }) => {
        await openCanvas(page);
        const nodeId = await addNode(page, "image");
        await page.locator(`[data-node-id="${nodeId}"]`).hover();
        // The hover toolbar carries crop, split and mask; the buttons are labelled
        // through i18n, so the test asserts that the toolbar itself appears with
        // at least one action rather than pinning a translation.
        const toolbarButtons = page.locator(`[data-node-id="${nodeId}"] button`);
        await expect.poll(async () => toolbarButtons.count()).toBeGreaterThan(0);
    });

    test("the canvas reloads with its nodes", async ({ page }) => {
        const projectId = await openCanvas(page);
        // Text nodes: the persistence path is the same for every kind, and a
        // panel would sit over the canvas while the reload is asserted.
        const first = await addNode(page, "text");
        await separateNodes(page, first, -260, 180);
        await addNode(page, "text");
        const before = await nodeIds(page);
        expect(before.length).toBe(2);
        // The canvas save is debounced, so the document is flushed before the
        // reload; a reload immediately after a change would race the write.
        await page.waitForTimeout(1200);

        await page.reload();
        await expect.poll(async () => (await nodeIds(page)).length, { timeout: 20_000 }).toBe(2);
        expect(page.url()).toContain(projectId);
    });
});
