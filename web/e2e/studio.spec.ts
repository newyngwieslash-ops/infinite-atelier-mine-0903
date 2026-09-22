import { expect, test, type Page } from "@playwright/test";

/**
 * The drama studio in browser mode.
 *
 * The studio's facts live in the Go core, so a Playwright run — which drives the
 * webview in a plain browser, with no Wails runtime injected — cannot observe a
 * single drama row. That is the point of this suite rather than a gap in it: what
 * a browser can verify is the behaviour the studio shows when the core is absent,
 * which is a supported way to run the app and therefore a state the interface
 * owes the user an honest answer for.
 *
 * So the tests below assert exactly three things, and nothing else:
 *
 *  1. The studio is reachable and says it needs the desktop application, instead
 *     of rendering a broken or a falsely empty page.
 *  2. The studio's navigation renders every section, and the sections with no
 *     implementation yet state which work package owns them. No test asserts a
 *     row of drama data, because a browser has none to give.
 *  3. The shell survives a direct navigation and a reload with no prior state.
 *
 * The with-core path — creating a project, listing episodes, projecting nodes —
 * needs the desktop build and is covered by the Go-side binding tests. Mocking
 * the Wails bindings here to fake that path would test a fiction.
 */

/** The studio list page, with the storage a previous test may have left cleared. */
async function openStudioList(page: Page) {
    await page.goto("/studio");
    await expect(page.locator("main")).toBeVisible();
}

/** Section ids the shell must render. Read from STUDIO_SECTIONS' order in the app. */
const SECTION_IDS = [
    "overview",
    "source",
    "story-graph",
    "script",
    "characters",
    "locations",
    "props",
    "director",
    "storyboard-table",
    "storyboard-canvas",
    "video",
    "audio",
    "timeline",
    "quality",
    "agents",
] as const;

/** Sections whose content is not built yet, with the package that owns each. */
const UNAVAILABLE_SECTIONS: Record<string, string> = {
    // WP-09 built `director` and `storyboard-table`, so the three that remain are the
    // media and export sections WP-11 owns. The list is what the shell renders against,
    // so removing an entry here is the same act as marking the section available — and the
    // test fails if the two disagree.
    video: "WP-11",
    audio: "WP-11",
    timeline: "WP-11",
};

test.describe("the drama studio without a core", () => {
    test("the project list renders its heading and the desktop-only notice", async ({ page }) => {
        await openStudioList(page);
        // The heading is translated, so the assertion is on the title's own
        // element rather than on a locale-specific string.
        await expect(page.locator("h1")).toBeVisible();
        await expect(page.locator("[data-testid='studio-desktop-only']")).toBeVisible();
        // The notice is an antd Alert: it states the requirement in text, so the
        // suite does not depend on an icon or a colour.
        await expect(page.locator("[data-testid='studio-desktop-only']")).toContainText(/\S/);
    });

    test("the top navigation carries a link to the studio", async ({ page }) => {
        await page.goto("/");
        const link = page.locator("nav a[href='/studio']").first();
        await expect(link).toBeVisible();
        await link.click();
        await expect(page).toHaveURL(/\/studio$/);
    });

    test("creating a project is refused with the desktop-only reason", async ({ page }) => {
        await openStudioList(page);
        // The implementation disables the entry when the bindings are absent, so
        // the behaviour to assert is that it is disabled — the button cannot be
        // pressed into a request that has nowhere to go.
        const button = page.locator("[data-testid='studio-new-project']");
        await expect(button).toBeVisible();
        await expect(button).toBeDisabled();
        // The empty state repeats the same entry, and it is disabled too.
        await expect(page.locator("[data-testid='studio-empty-create']")).toBeDisabled();
    });

    test("the empty state explains the studio and links to the free canvas", async ({ page }) => {
        await openStudioList(page);
        const empty = page.locator("[data-testid='studio-empty']");
        await expect(empty).toBeVisible();
        // The explanation is prose about what the studio is, so it is asserted as
        // non-empty text rather than as a translated string.
        await expect(empty.locator("p")).toContainText(/\S/);
        await expect(page.locator("[data-testid='studio-empty-open-canvas']")).toBeVisible();
        // And the link goes where it claims.
        await page.locator("[data-testid='studio-empty-open-canvas']").click();
        await expect(page).toHaveURL(/\/canvas$/);
    });

    test("the shell renders every section, and the unbuilt ones name their work package", async ({ page }) => {
        await page.goto("/studio/example-project");
        const nav = page.locator("[data-testid='studio-section-nav']");
        await expect(nav).toBeVisible();

        for (const id of SECTION_IDS) {
            await expect(nav.locator(`[data-section='${id}']`)).toHaveCount(1);
        }

        // The two sets are told apart by the attribute the shell publishes, not
        // by a list this test keeps: a section marked unavailable must render an
        // empty state, and one marked available must not.
        await expect(nav.locator("[data-section-available='false']")).toHaveCount(Object.keys(UNAVAILABLE_SECTIONS).length);
        const availableCount = await nav.locator("[data-section-available='true']").count();
        expect(availableCount).toBe(SECTION_IDS.length - Object.keys(UNAVAILABLE_SECTIONS).length);

        // The honest empty state is verified rather than assumed: each unbuilt
        // section must say which package owns it when it is opened.
        for (const [id, workPackage] of Object.entries(UNAVAILABLE_SECTIONS)) {
            const entry = nav.locator(`[data-section='${id}']`);
            await expect(entry).toHaveAttribute("data-section-available", "false");
            await entry.click();
            const emptyState = page.locator("[data-empty-section]");
            await expect(emptyState).toBeVisible();
            await expect(emptyState).toHaveAttribute("data-empty-section-work-package", workPackage);
            // The package is visible as text too, not only as an attribute, so a
            // person reading the page sees the claim this test checks.
            await expect(emptyState.locator("[data-empty-section-work-package-label]")).toContainText(workPackage);
        }
    });

    test("clicking a section marks it current without leaving the page", async ({ page }) => {
        await page.goto("/studio/example-project");
        const nav = page.locator("[data-testid='studio-section-nav']");
        await expect(nav).toBeVisible();
        const urlBefore = page.url();

        // Overview is the default, so a different one is chosen to prove the
        // click did the change.
        const target = nav.locator("[data-section='quality']");
        await target.click();
        await expect(target).toHaveAttribute("aria-current", "true");
        // The previously current entry must have given it up, or "current" would
        // mean nothing.
        await expect(nav.locator("[data-section='overview']")).not.toHaveAttribute("aria-current", "true");
        // The section is a view state, not a route: the URL is unchanged.
        expect(page.url()).toBe(urlBefore);
        await expect(page.locator("[data-active-section='quality']")).toBeVisible();
    });

    test("a section backed by the core states that it needs the core", async ({ page }) => {
        await page.goto("/studio/example-project");
        const nav = page.locator("[data-testid='studio-section-nav']");
        await expect(nav).toBeVisible();
        // The script section reads episodes from Go; with no core it must not
        // pretend the project has none.
        await nav.locator("[data-section='script']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        // A section that needs no query — the unbuilt ones — still renders its
        // empty state in the same session, so the two answers stay distinct. WP-09 built
        // `director`, so the section that answers this way is now one of WP-11's.
        await nav.locator("[data-section='video']").click();
        await expect(page.locator("[data-empty-section]")).toBeVisible();
        // And `director`, which WP-09 built, answers like the other core-backed sections
        // rather than claiming no query exists: the shell's no-core notice is the honest
        // answer for a section whose rows come from Go.
        await nav.locator("[data-section='director']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        await nav.locator("[data-section='storyboard-table']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
    });

    test("the import flow and the story graph state that they need the core", async ({ page }) => {
        // Both sections were filled by WP-06. With no core the SHELL answers for
        // them — it renders the no-core notice before reaching any section body —
        // so what matters here is that they are marked available and that the
        // shell's answer is the honest one. The sections' own empty states are
        // reached only in a desktop build, where the Go-side binding tests cover
        // them.
        await page.goto("/studio/example-project");
        const nav = page.locator("[data-testid='studio-section-nav']");
        await expect(nav).toBeVisible();
        // Both report themselves as available: they ARE built now, and marking
        // them unavailable would be the opposite lie.
        await expect(nav.locator("[data-section='source']")).toHaveAttribute("data-section-available", "true");
        await expect(nav.locator("[data-section='story-graph']")).toHaveAttribute("data-section-available", "true");

        await nav.locator("[data-section='source']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        // The WP-05 gap notice must be gone: it claimed no query could list the
        // fact layer, and WP-06 added them. Asserting its absence keeps the old
        // notice from being restored by accident.
        await nav.locator("[data-section='story-graph']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        await expect(page.locator("[data-testid='studio-story-graph-gap']")).toHaveCount(0);
    });

    test("a direct navigation and a reload both keep the shell working", async ({ page }) => {
        // No prior navigation and no prior state: this is the cold path the shell
        // must tolerate, since the id is taken from the URL alone.
        await page.goto("/studio/example-project");
        await expect(page.locator("[data-testid='studio-section-nav']")).toBeVisible();
        await expect(page.locator("[data-section='overview']")).toHaveAttribute("aria-current", "true");

        await page.reload();
        await expect(page.locator("[data-testid='studio-section-nav']")).toBeVisible();
        // The section resets to the default on reload, which is the documented
        // behaviour: the section is UI state and the store is not persisted.
        await expect(page.locator("[data-section='overview']")).toHaveAttribute("aria-current", "true");
    });

    test("the agents section is reachable and states that its records need the core", async ({ page }) => {
        // WP-07's Agent Center. In a browser the section is marked available —
        // it has real content in a desktop build — and the shell answers for it
        // with the no-core notice, which is the honest state rather than a table
        // of runs that would read as "this project has run nothing".
        await page.goto("/studio/example-project");
        const nav = page.locator("[data-testid='studio-section-nav']");
        await expect(nav).toBeVisible();
        await nav.locator("[data-section='agents']").click();
        await expect(page.locator("[data-active-section='agents']")).toBeVisible();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
    });

    test("the shell offers the way back and the way to the free canvas", async ({ page }) => {
        await page.goto("/studio/example-project");
        await expect(page.locator("[data-testid='studio-back-to-list']")).toBeVisible();
        await expect(page.locator("[data-testid='studio-open-canvas']")).toBeVisible();
        await page.locator("[data-testid='studio-back-to-list']").click();
        await expect(page).toHaveURL(/\/studio$/);
    });
});
