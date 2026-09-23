import type { desktop } from "@/wailsjs/go/models";

/**
 * backup.ts is the client for the project data backup binding.
 *
 * # Which backup this is
 *
 * `BackupBinding` is the Go core's ordinary backup: the project database and the
 * object store, written as one archive. It is NOT the browser-local package
 * `services/backup-restore.ts` produces. The two carry different data and are
 * restored by different machinery, so a caller must not read one for the other —
 * the panel that offers this one says so, in the tab beside the browser one.
 *
 * # The unavailable case
 *
 * The split `media.ts`, `drama.ts` and `memory.ts` all keep: a COMMAND throws
 * when the core is absent, and a QUERY answers with the honest "nothing" for its
 * question. `backupStateHeld` is the one query here — a browser session holds no
 * displaced restore state, so `false` is the true answer and lets a panel render
 * no notice rather than an error about a machine the interface never asked.
 *
 * `previewBackup` is deliberately a COMMAND even though it writes nothing. Its
 * answer is a verdict about one specific archive, so an "empty" placeholder would
 * be a claim about that archive — "0 projects, 0 assets" — which is worse than a
 * message saying this build cannot read it. The same reasoning rules out
 * placeholder DTOs for the calls that return results.
 */

/** Methods the binding must have before a backup can be written and applied. */
const REQUIRED_BACKUP_METHODS = ["ExportBackup", "RestoreBackup"] as const;

type DesktopWindow = Window & {
    go?: {
        desktop?: Record<string, Record<string, (...args: never[]) => unknown>>;
    };
};

function getDesktopWindow(): DesktopWindow | undefined {
    if (typeof window === "undefined") return undefined;
    return window as DesktopWindow;
}

/**
 * isBackupBindingsAvailable reports whether the core can back up and restore.
 *
 * It checks the two ends of the round trip rather than all five methods: a build
 * whose binding predates the preview or the discard step still writes and applies
 * an archive, and it fails on the specific call — which says more than a blanket
 * "unavailable". The reverse would be worse: a probe demanding all five would hide
 * a working export behind a restore that does not exist.
 */
export function isBackupBindingsAvailable(): boolean {
    const backup = getDesktopWindow()?.go?.desktop?.BackupBinding;
    if (!backup) return false;
    return REQUIRED_BACKUP_METHODS.every((method) => typeof backup[method] === "function");
}

function unavailableError(): Error {
    return new Error("The desktop core is not available in this window, so project data cannot be backed up or restored.");
}

let backupModule: Promise<typeof import("@/wailsjs/go/desktop/BackupBinding")> | undefined;

async function loadBackupBinding() {
    backupModule ??= import("@/wailsjs/go/desktop/BackupBinding").catch((error: unknown) => {
        backupModule = undefined;
        throw error;
    });
    return backupModule;
}

/**
 * exportBackup writes a backup and returns the archive as base64.
 *
 * It returns TEXT rather than saving anything: the binding never writes a
 * user-visible file (`internal/desktop/backup_binding.go` says so), and the
 * download is the webview's own act. A caller decodes the value to bytes and
 * hands them to its download path.
 *
 * The archive carries no credential by construction — the ordinary backup path
 * never reads the credential store (ADR-0006 §9) — so the base64 value is safe to
 * hold in memory and write to a file the user chooses.
 */
export async function exportBackup(): Promise<string> {
    if (!isBackupBindingsAvailable()) throw unavailableError();
    const binding = await loadBackupBinding();
    return binding.ExportBackup();
}

/**
 * previewBackup validates an archive and reports what restoring it would do.
 *
 * It touches nothing live and leaves nothing staged: the binding discards what
 * the validation staged, precisely so that asking this question twice is safe.
 * That is what makes it the step BEFORE a confirmation rather than after one —
 * the user reads what the archive holds before they agree to replace their work
 * with it.
 */
export async function previewBackup(archiveBase64: string): Promise<desktop.BackupPreview> {
    if (!isBackupBindingsAvailable()) throw unavailableError();
    const binding = await loadBackupBinding();
    return binding.PreviewBackup(archiveBase64);
}

/**
 * restoreBackup replaces this application's project data with the archive's.
 *
 * `confirm` is forwarded as given and must be true: the boundary refuses a false
 * value before it decodes anything, and the user's own consent is what that flag
 * records. A caller therefore reaches this only after showing them the preview.
 *
 * The core CLOSES the database to move its file, so the result's
 * `restartRequired` is always true and a caller must say so. The displaced data is
 * kept at `previousDatabasePath` until `discardBackupState` removes it, which is
 * the only irreversible act in the sequence.
 */
export async function restoreBackup(archiveBase64: string, confirm: boolean): Promise<desktop.RestoreResult> {
    if (!isBackupBindingsAvailable()) throw unavailableError();
    const binding = await loadBackupBinding();
    return binding.RestoreBackup(archiveBase64, confirm);
}

/**
 * backupStateHeld reports whether a previous restore's displaced data is still on disk.
 *
 * A QUERY: an absent core has displaced nothing, so a browser session is answered
 * `false` — which renders no notice rather than an error about a restore that
 * never happened here. That answer is for a window with NO core, and only for
 * that: a build whose binding predates this method satisfies the probe and fails
 * on the call, which is the same trade the other clients record. The alternative
 * — answering `false` when the machine was asked and could not answer — would be
 * a panel telling a user there is nothing to recover when it does not know.
 */
export async function backupStateHeld(): Promise<boolean> {
    if (!isBackupBindingsAvailable()) return false;
    const binding = await loadBackupBinding();
    return binding.BackupStateHeld();
}

/**
 * discardBackupState removes the data a restore displaced.
 *
 * A COMMAND, and the only irreversible act of the sequence: afterwards the
 * previous projects are gone. A caller must therefore ask the user plainly before
 * reaching here — the displaced copy is the only copy of their previous work
 * until they have opened a project and seen that the restored data is what they
 * wanted.
 */
export async function discardBackupState(): Promise<void> {
    if (!isBackupBindingsAvailable()) throw unavailableError();
    const binding = await loadBackupBinding();
    return binding.DiscardBackupState();
}

/**
 * readArchiveAsBase64 reads a chosen archive for the binding.
 *
 * A Wails call carries text, so the zip travels as base64. FileReader is used
 * rather than a manual loop, as `migration.ts` does for the same reason: it
 * handles the whole file in one pass and reports a failure instead of producing a
 * truncated string that the core would reject as a bad archive.
 */
export function readArchiveAsBase64(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(reader.error ?? new Error("The backup file could not be read."));
        reader.onload = () => {
            const result = reader.result;
            if (typeof result !== "string") {
                reject(new Error("The backup file could not be read."));
                return;
            }
            // FileReader's data URL form is `data:<type>;base64,<payload>`; the
            // binding wants the payload alone.
            const separator = result.indexOf(",");
            resolve(separator >= 0 ? result.slice(separator + 1) : result);
        };
        reader.readAsDataURL(file);
    });
}

/**
 * base64ToBytes decodes an exported archive for download.
 *
 * The loop is written out rather than spreading the string into
 * `String.fromCharCode(...)`, which overflows the call stack on a large archive —
 * the same size problem `drama.ts` avoids on the encoding side.
 */
export function base64ToBytes(base64: string): Uint8Array<ArrayBuffer> {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
        bytes[index] = binary.charCodeAt(index);
    }
    return bytes;
}

/**
 * backupArchiveName names the file an export is downloaded as.
 *
 * The name carries the moment the archive was WRITTEN, so a directory holding
 * several of them says which is which without opening any — which matters here
 * because this archive is the thing a user checks before replacing their work
 * with it. It is local time, deliberately: the person naming the file is the
 * person reading the clock, and a UTC stamp would name back-ups for the previous
 * day for anyone east of Greenwich in the morning.
 */
export function backupArchiveName(madeAt: Date): string {
    const pad = (value: number) => String(value).padStart(2, "0");
    const stamp = `${madeAt.getFullYear()}${pad(madeAt.getMonth() + 1)}${pad(madeAt.getDate())}-${pad(madeAt.getHours())}${pad(madeAt.getMinutes())}${pad(madeAt.getSeconds())}`;
    return `infinite-atelier-backup-${stamp}.zip`;
}
