import { resetDramaClients } from "../desktop/drama";

/**
 * The fake Wails surface the desktop clients are tested against.
 *
 * It installs the shape Wails generates — `window.go.desktop.<Binding>.<Method>` — which is the only
 * thing the clients' probes look at, and restores whatever `window` was on the way out so a test that
 * needed a core cannot leak into the absence tests that follow it. Each spec that needs a core imports
 * this rather than carrying its own copy: the helper was written three times as the suite grew, and a
 * shared one is what keeps the absence and presence directions phrased against the same window shape.
 *
 * It is not a `.spec.ts` file on purpose — the runner bundles every spec in this directory, and this
 * module is a fixture rather than a suite.
 *
 * The drama client's cached binding import is released in the `finally` block. That cache holds the
 * module Wails generated, which resolves against whatever `window` existed when it was first loaded,
 * so leaving it in place would let one test's fake answer another test's call.
 */
export type FakeBindings = Record<string, Record<string, (...args: never[]) => unknown>>;

/** withDesktopCore runs a body with a fake Wails binding surface installed. */
export async function withDesktopCore(bindings: FakeBindings, body: () => Promise<void> | void) {
    const host = globalThis as { window?: unknown };
    const previous = host.window;
    host.window = { go: { desktop: bindings } };
    try {
        await body();
    } finally {
        if (previous === undefined) {
            delete host.window;
        } else {
            host.window = previous;
        }
        resetDramaClients();
    }
}
