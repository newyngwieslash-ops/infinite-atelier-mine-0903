import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The backup client's two contracts.
 *
 * `backup.ts` keeps the split every desktop client here keeps: a COMMAND throws
 * when the core is absent, and a QUERY answers with the honest "nothing" for its
 * question. Both directions are asserted, because each alone is a mutation
 * waiting to happen — a probe hardcoded to `false` would hide a working backup
 * forever, and one hardcoded to `true` would offer a restore that cannot reach
 * anything.
 *
 * The absence direction is the one a browser-mode run can observe at all: the e2e
 * suite runs with no Wails runtime, so the panel's own body is never reached
 * there. The presence direction uses the shared `withDesktopCore` helper, which
 * installs the shape Wails generates.
 *
 * `backupStateHeld` is the one query. It answers `false` with no core because a
 * browser session has displaced nothing by a restore — which is what lets the
 * panel render no notice instead of an error about a machine it never asked.
 */
import {
    backupArchiveName,
    backupStateHeld,
    base64ToBytes,
    discardBackupState,
    exportBackup,
    isBackupBindingsAvailable,
    previewBackup,
    restoreBackup,
} from "../desktop/backup";
import { withDesktopCore, type FakeBindings } from "./desktop-core";

test("a window with no desktop core reports the backup binding unavailable", () => {
    assert.equal(isBackupBindingsAvailable(), false);
    // The panel gates every control on this probe, so the absence answer is the
    // whole of what a browser session sees: the no-core notice, and no button
    // that would press into a call with nowhere to go.
});

test("the held-state query answers false rather than throwing when the core is absent", async () => {
    // A QUERY. A browser session holds no data displaced by a restore, and an
    // exception here would render an error about a restore that never happened.
    assert.equal(await backupStateHeld(), false);
});

test("every backup command throws when the core is absent", async () => {
    // Each entry is a COMMAND: a user pressed a button, and a silent no-op would
    // leave them believing their project data had been written or replaced.
    const commands: Array<[string, () => Promise<unknown>]> = [
        ["exportBackup", () => exportBackup()],
        ["previewBackup", () => previewBackup("UEsDBAo=")],
        ["restoreBackup", () => restoreBackup("UEsDBAo=", true)],
        ["discardBackupState", () => discardBackupState()],
    ];
    for (const [name, run] of commands) {
        await assert.rejects(
            run(),
            (error: unknown) => {
                assert.ok(error instanceof Error, `${name} must reject with an Error`);
                assert.ok(error.message.length > 0, `${name}'s message must say something`);
                return true;
            },
            `${name} did not throw`,
        );
    }
});

test("a binding that predates the held-state query is treated as present and fails on the call", async () => {
    // The probe checks a representative set rather than every method, which is
    // deliberate and is the same trade `script-versions.spec.ts` records: an older
    // build with the backup surface and without `BackupStateHeld` still exports
    // and restores, and the specific call is what fails. Answering `false` here
    // instead would be the client inventing an answer to a question the machine
    // was asked and did not answer.
    await withDesktopCore({ BackupBinding: { ExportBackup: () => "", RestoreBackup: () => ({}) } }, async () => {
        assert.equal(isBackupBindingsAvailable(), true, "a binding without the new query is still available");
        await assert.rejects(
            backupStateHeld(),
            (error: unknown) => {
                assert.ok(error instanceof Error, "the failure must be an Error");
                return true;
            },
            "a build with no BackupStateHeld must fail on the call rather than answer false",
        );
    });
});

/**
 * The other direction: a window WITH a core.
 *
 * The fake installs the same shape Wails generates, `window.go.desktop.<Binding>.<Method>`,
 * and `withDesktopCore` restores whatever `window` was on the way out so the
 * absence tests above cannot be affected by one that ran before them.
 */
test("a window with a backup binding reports it available and carries calls through", async () => {
    const calls: Array<[string, unknown[]]> = [];
    const core: FakeBindings = {
        BackupBinding: {
            ExportBackup: () => {
                calls.push(["ExportBackup", []]);
                return "UEsDBAo=";
            },
            PreviewBackup: (archive: unknown) => {
                calls.push(["PreviewBackup", [archive]]);
                return {
                    manifestVersion: 1,
                    appVersion: "1.0.0",
                    schemaVersion: 7,
                    createdAt: "2026-09-22T10:00:00Z",
                    projects: 3,
                    assets: 12,
                    files: 40,
                    databaseBytes: 1024,
                    fileBytes: 2048,
                    stagedFiles: 40,
                    stagedDatabase: "C:/tmp/staged.db",
                };
            },
            RestoreBackup: (archive: unknown, confirm: unknown) => {
                calls.push(["RestoreBackup", [archive, confirm]]);
                return {
                    manifestVersion: 1,
                    projects: 3,
                    assets: 12,
                    files: 40,
                    databasePath: "C:/data/app.db",
                    previousDatabasePath: "C:/data/app.db.pre-restore",
                    rolledBack: false,
                    restartRequired: true,
                };
            },
            DiscardBackupState: () => {
                calls.push(["DiscardBackupState", []]);
                return undefined;
            },
            BackupStateHeld: () => {
                calls.push(["BackupStateHeld", []]);
                return true;
            },
        },
    };

    await withDesktopCore(core, async () => {
        assert.equal(isBackupBindingsAvailable(), true, "a complete backup binding must read as available");

        const archive = await exportBackup();
        assert.equal(archive, "UEsDBAo=", "the archive the core returned must be the one answered");

        const preview = await previewBackup(archive);
        assert.equal(preview.projects, 3, "the preview the core returned must be the one answered");
        assert.equal(preview.assets, 12);
        assert.equal(preview.createdAt, "2026-09-22T10:00:00Z");

        const result = await restoreBackup(archive, true);
        assert.equal(result.restartRequired, true, "a restart requirement must travel back as reported");
        assert.equal(result.previousDatabasePath, "C:/data/app.db.pre-restore");
        assert.equal(result.rolledBack, false);

        assert.equal(await backupStateHeld(), true, "a held state must travel back as reported");

        await discardBackupState();

        assert.deepEqual(
            calls.map(([name]) => name),
            ["ExportBackup", "PreviewBackup", "RestoreBackup", "BackupStateHeld", "DiscardBackupState"],
        );
        // The archive and the flag must reach the binding AS GIVEN: `confirm` is
        // the user's own consent, and a client that decided it — or dropped it —
        // would restore over work nobody agreed to replace.
        assert.deepEqual(calls[1][1], ["UEsDBAo="]);
        assert.deepEqual(calls[2][1], ["UEsDBAo=", true]);
    });
});

test("the probe reads available only when both ends of the round trip are installed", async () => {
    // The probe names ExportBackup and RestoreBackup. One alone is not a backup
    // path: an export with no restore writes an archive nothing can apply, and a
    // restore with no export can only be reached with somebody else's file.
    await withDesktopCore({ BackupBinding: { ExportBackup: () => "" } }, () => {
        assert.equal(isBackupBindingsAvailable(), false, "an export with no restore must not read as available");
    });
    await withDesktopCore({ BackupBinding: { RestoreBackup: () => ({}) } }, () => {
        assert.equal(isBackupBindingsAvailable(), false, "a restore with no export must not read as available");
    });
    await withDesktopCore({ BackupBinding: {} }, () => {
        assert.equal(isBackupBindingsAvailable(), false, "a binding with no methods must not read as available");
    });
    await withDesktopCore({}, () => {
        assert.equal(isBackupBindingsAvailable(), false, "a window with no bindings must not read as available");
    });
});

test("a rolled-back restore travels back as rolled back rather than as a success", async () => {
    // `rolledBack: true` means the promotion failed and the previous state was put
    // back. The panel renders it as a failure, and that is only possible if the
    // client forwards the field rather than flattening the result to "it returned".
    const rollingBack: FakeBindings = {
        BackupBinding: {
            ExportBackup: () => "",
            RestoreBackup: () => ({
                manifestVersion: 1,
                projects: 0,
                assets: 0,
                files: 0,
                databasePath: "",
                rolledBack: true,
                restartRequired: true,
            }),
        },
    };
    await withDesktopCore(rollingBack, async () => {
        const result = await restoreBackup("UEsDBAo=", true);
        assert.equal(result.rolledBack, true, "the rollback flag must not be dropped");
        assert.equal(result.previousDatabasePath ?? "", "", "a rolled-back restore names no displaced copy");
    });
});

/**
 * The two pure helpers, which are the parts of the export path a browser CAN run.
 *
 * `base64ToBytes` is what the download hands to `saveAs`: a decoding that dropped
 * the high bit would produce an archive the core refuses, so the round trip is
 * asserted against a payload with bytes above 0x7f rather than against ASCII
 * alone.
 */
test("base64ToBytes decodes the archive without dropping the high bit", () => {
    // "UEsDBAo=" is PK\x03\x04\n, the local-file header every zip opens with.
    assert.deepEqual([...base64ToBytes("UEsDBAo=")], [0x50, 0x4b, 0x03, 0x04, 0x0a]);
    // 0xff 0xfe 0xfd — all above 0x7f, which is the range a naive implementation
    // loses first.
    assert.deepEqual([...base64ToBytes("//79")], [0xff, 0xfe, 0xfd]);
    assert.deepEqual([...base64ToBytes("")], []);
});

test("the archive name carries the moment it was written", () => {
    const name = backupArchiveName(new Date(2026, 8, 22, 9, 5, 3));
    assert.equal(name, "infinite-atelier-backup-20260922-090503.zip");
    // Local time and zero-padded: a back-up taken in the morning names its own day,
    // and one taken at 09:05 is not written as "95".
    assert.equal(backupArchiveName(new Date(2026, 0, 1, 0, 0, 0)), "infinite-atelier-backup-20260101-000000.zip");
});
