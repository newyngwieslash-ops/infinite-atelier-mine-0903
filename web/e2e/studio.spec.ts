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
    "memory",
] as const;

/**
 * Sections whose content is not built yet, with the package that owns each.
 *
 * CHANGED BY WP-11: this map held `video`, `audio` and `timeline` — the three media and export
 * sections — and WP-11 built all three, so it is now EMPTY. It stays as a named map rather than
 * being deleted because the count assertions below are phrased against it, and an empty map is the
 * honest statement: every section the shell lists is backed by real commands.
 *
 * The placeholder component (`StudioEmptySection`) is still where it was and is still reachable from
 * the shell's `default:` branch, so this map is also what would carry a section again if one were
 * ever added to `STUDIO_SECTIONS` without a body.
 */
const UNAVAILABLE_SECTIONS: Record<string, string> = {};

/**
 * The three sections WP-11 built, each with the test id of the body it now renders.
 *
 * In a browser session the SHELL answers for an available section before reaching any body — it
 * renders the no-core notice — so a browser-mode run cannot observe these roots. They are named here
 * anyway because the run asserts the shell's answer is the core-backed one rather than the
 * placeholder, and that distinction is only meaningful with the sections listed.
 */
const WP11_SECTIONS = ["video", "audio", "timeline"] as const;

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

        // WP-11 made `video`, `audio` and `timeline` real, so the honest assertion is that they are
        // marked available and that opening one does NOT reach the placeholder. The old form of this
        // test ran the loop below over those three; a loop over an empty map would assert nothing, so
        // the replacement states the new fact directly instead of leaving a vacuous loop behind.
        for (const id of WP11_SECTIONS) {
            await expect(nav.locator(`[data-section='${id}']`)).toHaveAttribute("data-section-available", "true");
            await nav.locator(`[data-section='${id}']`).click();
            await expect(page.locator(`[data-active-section='${id}']`)).toBeVisible();
            await expect(page.locator("[data-empty-section]")).toHaveCount(0);
        }

        // The honest empty state is verified rather than assumed: each unbuilt section must say
        // which package owns it when it is opened. This runs over whatever the map holds, which is
        // nothing at present — and the loop body is what keeps the check in place if a future
        // section is added unavailable.
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
        // CHANGED BY WP-11: this case used to click `video` and assert the PLACEHOLDER, because
        // `video` was one of WP-11's unbuilt sections. WP-11 built all three, so a media section now
        // answers like every other core-backed one — and `video` is the section that proves it, since
        // a media read with no core is exactly the case that must not render as "this episode has no
        // shots". The placeholder assertion moved to the available-sections check above, where it now
        // asserts the OPPOSITE (no placeholder) for these three ids.
        await nav.locator("[data-section='video']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        await expect(page.locator("[data-empty-section]")).toHaveCount(0);
        await nav.locator("[data-section='audio']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        await nav.locator("[data-section='timeline']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
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
        await nav.locator("[data-section='story-graph']").click();
        await expect(page.locator("[data-testid='studio-section-no-core']")).toBeVisible();
        // THE WP-05 GAP NOTICE IS NOT ASSERTED HERE, and that is the correction WP-17 made.
        //
        // This test used to assert `studio-story-graph-gap` had a count of 0. That testid lived in
        // `pages/studio/sections.tsx`, and WP-06 moved the section into
        // `components/studio/story-graph-view.tsx` and deleted the notice. Nothing pointed the
        // assertion at the new file, so from WP-06 onward it asserted the absence of an element no
        // file could produce — and a negative assertion against a testid that does not exist can
        // never fail. It was a passing test that checked nothing for eleven work packages.
        //
        // It cannot be repaired from THIS surface, which is the part worth writing down: the shell
        // returns the no-core notice before the section switch runs (`pages/studio/project.tsx`),
        // so `StoryGraphSection` is never MOUNTED in browser mode and no assertion here can observe
        // anything inside it. The facts that replaced the old gap ARE asserted above — the section
        // reports itself available and the shell says why it cannot render — and the section's own
        // states are covered by the desktop build's tests.
        //
        // What would be worse than deleting it is keeping a look-alike: an assertion that the
        // section's `data-testid="studio-story-graph-section"` is absent would be exactly as
        // vacuous for exactly the same reason.
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

    test("the memory section is reachable and states that its records need the core", async ({ page }) => {
        // The memory center is WP-10's surface. Without a core it must say so rather than render an
        // empty list: "the core could not be reached" and "nothing is remembered" are different
        // situations, and only one of them is true here — which is the rule every section keeps.
        // A project route, because the section nav renders inside a project's shell rather than on
        // the list: the memory center reads one project's memories, so the shell it lives in is the
        // one that has a project.
        await page.goto("/studio/example-project");
        const nav = page.getByTestId("studio-section-nav");
        await expect(nav).toBeVisible();
        await nav.locator("[data-section='memory']").click();

        await expect(page.locator("[data-active-section='memory']")).toBeVisible();
        // The SHELL's no-core notice, because the shell answers for any available section before the
        // section's own body runs. The section carries its own guard for the partial case — a build
        // with the drama binding and no memory binding — which this browser-mode run cannot reach,
        // and asserting the wrong one of the two would leave that case untested while looking tested.
        await expect(page.getByTestId("studio-section-no-core")).toBeVisible();
        // No table, because there is nothing to put in one.
        await expect(page.getByTestId("studio-memory-table")).toHaveCount(0);
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
