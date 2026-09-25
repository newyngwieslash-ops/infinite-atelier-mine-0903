import type { desktop } from "@/wailsjs/go/models";

/**
 * diagnostics.ts is the client for FR-180's diagnostics bundle.
 *
 * # The two-call shape, and why it is not one call with a flag
 *
 * `planDiagnostics` is a READ that produces the manifest a user decides on; `collectDiagnostics`
 * assembles what they chose. That is SECURITY 14.2's 「生成前展示清单；用户可取消内容」 expressed as
 * an API rather than as a dialog: a plan is safe to ask twice, and the collection is the ANSWER to
 * the question the plan asked.
 *
 * # The unavailable case
 *
 * This client has no QUERIES in the `media.ts` sense — both calls are commands, because both produce
 * a verdict about this machine rather than an empty answer. A browser session therefore gets a probe
 * to render its unavailable state with, and the calls throw.
 */

type DesktopWindow = Window & {
    go?: {
        desktop?: Record<string, Record<string, (...args: never[]) => unknown>>;
    };
};

function getDesktopWindow(): DesktopWindow | undefined {
    if (typeof window === "undefined") return undefined;
    return window as DesktopWindow;
}

let diagnosticsModule: Promise<typeof import("@/wailsjs/go/desktop/DiagnosticsBinding")> | undefined;

async function loadDiagnosticsBinding() {
    diagnosticsModule ??= import("@/wailsjs/go/desktop/DiagnosticsBinding").catch((error: unknown) => {
        diagnosticsModule = undefined;
        throw error;
    });
    return diagnosticsModule;
}

/**
 * isDiagnosticsAvailable reports whether this build can produce a bundle.
 *
 * It checks the two methods rather than one, because a build whose binding predates the collection
 * would answer a plan and fail on the collection — a panel that offered the button anyway would be
 * offering a control that cannot work.
 */
export function isDiagnosticsAvailable(): boolean {
    const binding = getDesktopWindow()?.go?.desktop?.DiagnosticsBinding;
    return Boolean(binding) &&
        typeof binding?.PlanDiagnostics === "function" &&
        typeof binding?.CollectDiagnostics === "function";
}

function unavailableError(): Error {
    return new Error("Diagnostics need the desktop application; this browser session has no core to read them from.");
}

/** resetDiagnosticsClients clears the cached import so tests can re-resolve it. */
export function resetDiagnosticsClients(): void {
    diagnosticsModule = undefined;
}

/**
 * planDiagnostics reports what a bundle would contain.
 *
 * An EMPTY section list asks for everything, which is what a panel wants for its opening manifest: a
 * user chooses what to remove rather than what to add, so nothing is hidden by default.
 */
export async function planDiagnostics(sections: string[]): Promise<desktop.DiagnosticsPlanDTO> {
    if (!isDiagnosticsAvailable()) throw unavailableError();
    const binding = await loadDiagnosticsBinding();
    return binding.PlanDiagnostics(sections);
}

/** collectDiagnostics assembles the bundle a user agreed to. */
export async function collectDiagnostics(sections: string[]): Promise<desktop.DiagnosticsBundleDTO> {
    if (!isDiagnosticsAvailable()) throw unavailableError();
    const binding = await loadDiagnosticsBinding();
    return binding.CollectDiagnostics(sections);
}
