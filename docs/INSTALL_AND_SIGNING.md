# Install and Signing Strategy — WP-12 Item 9

**Status: strategy, not delivery.** This document records what the build produces today, what a
signed release would require, and what is therefore **not done**. Where a fact was checked, the
command or `file:line` is given; where something is unknown, this document says so.

Item 9 of `docs/ROADMAP.md`'s WP-12 scope is "installer/signing strategy". A strategy can be
delivered as a document. A **certificate cannot**: it costs money and needs a legal identity, and
this repository has neither. Every statement below is written to keep that distinction visible.

---

## 1. What the build produces today

### 1.1 A single portable executable, and nothing else

`wails build` produces **one file**:

```text
build/bin/InfiniteAtelier.exe     33,092,608 bytes, PE32+ x64 (machine 0x8664)
```

Verified by `ls -la build/bin/` and by reading the PE header's `Machine` field. There is no
directory of side-by-side DLLs, no `resources/` tree, and no second artifact.

`wails.json` fixes the pieces that decide the name and the metadata:

| Field | Value | Effect |
|---|---|---|
| `outputfilename` | `InfiniteAtelier` | the `.exe` basename |
| `name` | `源铭振跃` | the Wails project name, and the window title (`main.go:79`) |
| `info.productName` | `源铭振跃` | the executable's `ProductName` resource |
| `info.productVersion` | `1.0.0` | the executable's `ProductVersion` resource |
| `info.copyright` | `Copyright © 2026 GuiYi-Xi` | the executable's `LegalCopyright` resource |
| `wailsjsdir` | `web/src` | where bindings are generated |
| `frontend:dir` | `web` | the frontend the binary embeds |

The version resource is **real and present**, verified by locating the UTF-16
`VS_VERSION_INFO` strings inside the built binary: `CompanyName` and `ProductName` and
`FileDescription` all read `源铭振跃`, `ProductVersion` reads `1.0.0`, and `LegalCopyright` reads
`Copyright © 2026 GuiYi-Xi`. `build/windows/info.json` is the template that feeds them, and
`build/windows/wails.exe.manifest` declares per-monitor-v2 DPI awareness and the common-controls v6
dependency.

**The frontend is embedded, not loaded from disk.** `main.go:25` is
`//go:embed all:web/dist`, so the executable carries the built React bundle (`web/dist/assets/index-*.js`
is visible in the binary's strings) and needs no Node.js and no Vite on the target machine. README's
claim to this effect is accurate.

The build is **reproducible from the repository** with the pinned toolchain:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
wails build
```

### 1.2 What the executable does NOT contain

- **THE INSTALLER IS NOW BUILT, AND THE GAP WAS INDEED ONE MISSING TOOL.** WP-15 established that
  `wails build -nsis` was wired and stopped at `Warning: Cannot create installer: makensis not found`,
  and it recorded the fix as "install NSIS 3.x". **WP-24 did that and the artifact exists.** The
  earlier note said `makensis` "is not available from the package managers present"; that check was
  incomplete — `winget` and `choco` are both on this host and both carry NSIS, and

  ```bash
  winget install --id NSIS.NSIS --accept-source-agreements --accept-package-agreements --silent
  ```

  installed **NSIS 3.12** to `C:/Program Files (x86)/NSIS/`. With it on `PATH`,
  `wails build -nsis` completed and produced:

  ```text
  Creating NSIS installer
  ------------------------------
    - Building 'amd64' installer: Done.
  ```

  The artifacts are `build/bin/源铭振跃-amd64-installer.exe` (14.9 MB) and
  `build/bin/InfiniteAtelier.exe` (31.9 MB), plus a bundled `MicrosoftEdgeWebview2Setup.exe` that the
  template fetches for the WebView2 runtime. **They are build outputs and stay untracked**
  (`build/bin/` is in `.gitignore`), which is why this section describes how to produce them rather
  than pointing at committed bytes.

  **What a release still requires is the CERTIFICATE, not the compiler**: the installer is unsigned
  (see below), and `makensis` being present does not change that.

- **THE USER'S PROJECTS SURVIVE AN UNINSTALL, AND THAT WAS VERIFIED RATHER THAN ASSUMED.** The
  generated uninstaller runs `RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"`, which reads like it removes
  the application's data. It does not remove the DATABASE, and the reason is that the two paths differ:
  `PRODUCT_EXECUTABLE` resolves to `${INFO_PROJECTNAME}.exe`, i.e. `源铭振跃.exe` from `wails.json`'s
  `name`, while the application stores everything under `os.UserConfigDir()/InfiniteAtelier` (see
  `internal/infrastructure/appdirs`). So the uninstaller clears a WebView2 data directory named after
  the PRODUCT and leaves `%AppData%\InfiniteAtelier\{app.db,files,logs,snapshots}` untouched.
  **A release that ever renamed the product to match `applicationDirectory` would delete user data on
  uninstall**, which is why the two names are recorded here side by side.

- **UPGRADE IS INSTALL-OVER, and the app's own migrations are what makes it safe.** The template
  overwrites the executable in place and does not remove the data directory, so an upgrade is: quit the
  application, run the newer installer, start it. The database is migrated forward on next start by
  `internal/infrastructure/database`'s embedded migrations, which are checksum-verified (a changed
  published migration is refused rather than applied) and forward-only. **A downgrade is NOT supported
  and not attempted**: an older binary meeting a newer `user_version` would need migrations that do not
  exist, and refusing is the safe answer.
- **No code-signing configuration.** `grep -rni "sign\|certificate\|signtool" wails.json package.json build/`
  returns **nothing** (exit status 2, i.e. no match in any of the three inputs). There is no
  `windows.certificate` block, no `signtool` invocation, no `SignTool` step in
  `.github/workflows/desktop-build.yml`.
- **No Authenticode signature on the built binary.** Read from the PE optional header's certificate
  table (data directory index 4): `rva=0 size=0`, which means no signature is attached. Windows will
  show this as an unknown publisher.
- **No uninstaller**, because there is nothing installed.

---

## 2. What `scripts/verify.sh` does about the build

`scripts/verify.sh:144-149` runs the Wails production build **only when the `wails` CLI is on
`PATH`**:

```sh
if command -v wails >/dev/null 2>&1; then
  cd "$ROOT_DIR"
  run_step "Wails production build" wails build
else
  printf 'SKIP: Wails production build — install pinned CLI v2.15.0 with: go install ...\n'
fi
```

**On this host the step is a SKIP.** `which wails` returns nothing, so the script prints that
message and exits successfully. This is honest and it is also a gap: the SKIP does not fail the
script, so a run of `verify.sh` on a machine without Wails CLI reports `PASS: available verification
gates completed` without having built the desktop binary at all. `scripts/verify.ps1` has the same
shape for the same step.

The same step runs **for real** in CI: `.github/workflows/desktop-build.yml:67-70` installs
`wails@v2.15.0` and runs `wails build` on `windows-latest`. **That workflow has not been executed** —
no push is authorized (AGENTS §5), so it was read, not run. Whether CI's `wails build` succeeds is
therefore **not verified** in this environment.

`build/bin/InfiniteAtelier.exe` on disk is dated `Sep 23 04:37` and proves *some* build ran on this
machine, but it is dated evidence rather than a gate: nothing re-runs it, and `build/bin/` is
gitignored (`.gitignore:30`), so the artifact is not part of the repository either.

---

## 3. What a signed release would require

Four things. **None of them exists here**, and each is stated as a requirement rather than a plan.

### 3.1 A code-signing certificate — a purchase, not a task

An Authenticode code-signing certificate from a publicly trusted CA. This is the item that cannot be
completed by writing code:

- It costs money, on a recurring basis (typically yearly).
- Since June 2023 the CA/Browser Forum requires the private key for an OV/EV code-signing
  certificate to live on a **FIPS 140-2 Level 2 or Common Criteria EAL4+ hardware token or HSM**.
  That means a physical token or a cloud signing service, not a file in a repository secret.
- It requires **a legal identity**. A CA verifies a business registration or, for an individual
  certificate, a government-issued identity. `wails.json` attributes the work to an individual
  (`GuiYi-Xi`); whether that identity is the one to be verified, and in which legal form, is a
  decision this repository cannot make for its owner.
- `wails.json`'s `info.copyright` and the `CompanyName` resource both currently read
  `源铭振跃`/`GuiYi-Xi`. A certificate's subject must be consistent with what the binary claims, so
  this is a decision with a code change attached.

**Plainly: this repository does not have a certificate, cannot obtain one, and has no identity it is
authorised to sign under.** No signing work is scheduled or partially done.

### 3.2 `signtool`, and where in the pipeline it would go

The signing tool ships with the Windows SDK (`signtool.exe`), invoked against the finished binary:

```text
wails build                     # produces build/bin/InfiniteAtelier.exe
signtool sign /fd SHA256 /tr <timestamp-url> /td SHA256 /a build/bin/InfiniteAtelier.exe
signtool verify /pa build/bin/InfiniteAtelier.exe
```

Two things this ordering implies, and both are decisions rather than details:

- **Signing is post-build and would have to be added to CI as its own step**, after
  `wails build` in `desktop-build.yml`. Wails v2 does not sign by itself; there is no
  `wails.json` key for it in this configuration.
- **A timestamp is required, not optional.** Without one, every signature becomes invalid when the
  certificate expires, and the already-shipped binary would stop verifying.
- **The certificate must not enter the repository.** It belongs in the CI provider's secret store or
  on a hardware token, never in a file — `docs/SECURITY.md` §1 and AGENTS §5 both forbid committing
  keys, and a signing key is the most damaging kind to leak.

### 3.3 A documented identity and a decision about what the signature attests

`wails.json`'s `info.copyright` says `GuiYi-Xi` and `LICENSE` is MIT. A signature attests "this
binary was produced by the holder of this certificate". For that statement to be meaningful, the
project needs to decide **who that is** and keep the copyright notice, the certificate subject and
the company name resource consistent. That is an owner decision, not an engineering one.

### 3.4 What signing would and would not change

- **It would stop the SmartScreen "unknown publisher" warning** for a binary whose certificate has
  earned reputation. A new certificate starts with no reputation, so the warning can persist for a
  while; signing alone is not a reputation bypass.
- **It would let a user verify the binary was not modified** after signing.
- **It would NOT make the application safer at runtime.** No security property in
  `docs/SECURITY.md` depends on the signature. This is a trust and distribution concern.

---

## 4. What an installer would need

If a distributable package is wanted, these are the decisions. **None is made here.**

### 4.1 The WebView2 runtime question — answered: the build assumes it is present

A Wails application renders through Microsoft Edge WebView2. Wails v2.15.0's default build strategy
is `download` (`cmd/wails/flags/build.go:71`: `WebView2: "download"`, wired to the
`wv2runtime.download` build tag), and `wails.json` sets no `webview2` override — the word does not
appear in it at all.

What that means in practice, for THIS build:

- The **minimum runtime version is `94.0.992.31`**, a constant in
  `wails/v2@v2.15.0/internal/wv2installer/wv2installer.go` (`MinimumRuntimeVersion`). That exact
  string is present in the built binary, and it is the value the "Minimum version required:" message
  would print. (The binary also contains `113.0.1774.30` and `100.0.1185.39`; those are capability
  thresholds from `go-webview2/pkg/edge/capabilities.go`, not runtime requirements — an earlier draft
  of this document misread an adjacent pair of them as a version.)
- If the runtime is **missing or too old**, the application does NOT silently fail. It shows a
  message box and offers to install. All four message variants are present in the built binary,
  matching Wails' `pkg/options/windows/windows.go:155-163`:
  - `The WebView2 runtime is required. Press Ok to download and install. Note: The installer will
    download silently so please wait.`
  - `The WebView2 runtime needs updating. Press Ok to download and install. …`
  - `This application requires the WebView2 runtime. Press OK to open the download page. Minimum
    version required: …`
  - `The WebView2 runtime is required to run this application. Please contact your system
    administrator.`
- `main.go` passes **no `Windows` options block**, so the defaults apply — which means the built
  binary downloads and installs the WebView2 bootstrapper at first run, rather than silently
  requiring an administrator to have pre-installed it.
- **The bootstrapper is not embedded.** No `MicrosoftEdgeWebview2Setup` / `wv2runtime.embed` string
  is present in the binary, consistent with the `download` strategy. So first launch on a machine
  without the runtime **needs a network connection**, and downloading implies the user consents to a
  Microsoft component being installed at that moment.

**The decision an installer would face, and it is a real one.** The `download` strategy is what
ships, but a machine that is offline, or behind a proxy, cannot use it. An installer would choose
between: bundling the Evergreen Standalone Installer (offline, ~150 MB more), the Evergreen
Bootstrapper (small, needs network), a fixed-version runtime distribution (largest, most control), or
documenting the prerequisite and doing nothing. Wails supports all four through the `webview2` build
flag; picking one is a product decision because it trades package size against offline installability.

### 4.2 Per-user versus per-machine — the recommendation, with its reasons

**Per-user is the correct default for this application**, and the reason is a fact about where the
data goes rather than a preference:

- The application stores everything under `os.UserConfigDir()/InfiniteAtelier`
  (`internal/infrastructure/appdirs/dirs.go:11,32`) — on Windows, `%AppData%\InfiniteAtelier`, with
  the directory created `0o700`. That path is resolved **per user**, not per machine. A per-machine
  install whose first run happens under one account would create the data directory for that
  account only, and a second user on the same machine would get a second, empty installation's worth
  of data.
- A per-machine install writes to `Program Files`, which needs elevation. The application reads and
  writes only inside its own user profile, so elevation buys nothing.
- Per-user installs do not require an administrator, which matters for a single-developer project
  with no IT department to approve one.

The cost, stated: a per-user install is per-account, so each Windows user re-installs. For a
single-user desktop application that is the right trade.

### 4.3 What an installer would install, and what it would not

Installing means: placing `InfiniteAtelier.exe`, creating a Start Menu shortcut, and (optionally) a
desktop shortcut. It does **not** mean creating the data directory — the application creates it on
first run, and an installer that pre-created it would have to decide which user's it was.

### 4.4 Uninstall behaviour

This is where an installer needs a decision it cannot take by default, because **uninstalling would
either destroy or orphan the user's work**:

- The application's data (`%AppData%\InfiniteAtelier` — `app.db`, `files/`, `temp/`, `logs/`,
  `snapshots/`) is **not part of the installation**. An uninstaller that removes only the program
  leaves every project behind, which is the safe default and the one to choose.
- Removing the data would delete the user's projects, storyboards and media with no undo. If it is
  offered at all it must be an explicit, separately-confirmed option — the same posture the
  application already takes for its one irreversible act
  (`DiscardBackupState`, `internal/desktop/backup_binding.go:251-264`).
- The **OS credential store entries** (`InfiniteAtelier:provider:<id>`,
  `internal/domain/provider/provider.go:132`) are another thing an uninstaller would have to decide
  about. Leaving them leaves orphaned credentials; deleting them silently is a security-relevant
  action a user did not ask for. The recommendation is to leave them and say so.
- The application's own **restore** command already keeps the displaced state until the user
  confirms it is good (`internal/desktop/backup_binding.go:186-192`). An uninstaller should hold the
  same standard, and there is currently no uninstaller to hold it.

---

## 5. What is therefore NOT done — the honest list

| Item | Status | Why |
|---|---|---|
| Portable executable build | **DONE** | `wails build`; artifact on disk; CI step exists (unrun). |
| Version/company/copyright resources in the binary | **DONE** | Verified in the built PE's version resource. |
| Reproducible pinned toolchain documented | **DONE** | README + `wails@v2.15.0`. |
| Build gate in `verify.sh` | **PARTIAL** | Runs when `wails` is on `PATH`; **SKIPs here**, and a SKIP is not a FAIL. |
| CI desktop build | **NOT VERIFIED** | `.github/workflows/desktop-build.yml:67-70` exists; no push authorized, so it has never run. |
| Code signing | **NOT DONE — BLOCKED BY A PURCHASE AND AN IDENTITY** | No certificate, no `signtool` step, no signature on the binary (PE certificate table is empty). |
| Installer | **NOT DONE** | No installer script, no packaging step, no product decision on per-user vs per-machine recorded anywhere but here. |
| WebView2 strategy decision | **NOT DONE — default in effect** | Wails' `download` strategy applies. The trade against bundling is undecided. |
| Uninstall behaviour | **NOT DONE** | Nothing to uninstall until an installer exists. |
| Windows clean-VM run | **NOT DONE — BLOCKED BY ENVIRONMENT** | No clean Windows VM available to this work package. Nothing in this document is evidence that the binary runs on a machine other than the one that built it. |

### What the absence of signing means for a release

A user who runs `InfiniteAtelier.exe` today sees an unknown-publisher warning and a Windows
SmartScreen prompt on a machine that has not seen the file before. That is a **distribution friction**
and a **trust** problem, and it is not one of `PRD.md:1734-1748`'s eleven release blockers, nor one of
`docs/SECURITY.md:706-724`'s fifteen. It is therefore a deferrable item for a Release Candidate
**provided the release says so** — which is what `docs/RELEASE_CHECKLIST.md` does.

---

## 6. What a person would run, in order, to produce and check a release build

Nothing below is automated end-to-end; this is the sequence a person follows.

```powershell
# 1. Toolchain (once per machine)
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
$env:PATH = "$(go env GOPATH)\bin;$env:PATH"

# 2. Build
wails build

# 3. Confirm the artifact and what it claims to be
Get-Item build\bin\InfiniteAtelier.exe
(Get-Item build\bin\InfiniteAtelier.exe).VersionInfo | Format-List *

# 4. Confirm the signature state (this is expected to report "NotSigned" today)
Get-AuthenticodeSignature build\bin\InfiniteAtelier.exe

# 5. Ship the ONE file, plus the notices a distribution must carry
#    build\bin\InfiniteAtelier.exe
#    THIRD_PARTY_NOTICES.md        (licence texts — see AGENTS §6)
#    sbom\cyclonedx.json           (machine-readable inventory)
```

**Step 4 is a check that currently fails by design**, and a release that ships without signing must
record that it did. **Step 5 is not optional**: AGENTS §6 and `THIRD_PARTY_NOTICES.md`'s own
preamble both say the notices file travels with a desktop distribution, and WP-12 item 10 generated
the SBOM that makes the inventory checkable.

### What has NOT been checked about the target machine

- **No clean-VM run has happened.** Nothing here is evidence that the binary starts on a machine
  other than this one.
- **The minimum supported Windows version is not stated anywhere in the repository**, and this
  document does not invent one. What is known: the binary is **PE32+ for x64** (`machine = 0x8664`,
  read from its header), so it is 64-bit x86 Windows only; and the Go toolchain version the module
  pins has its own Windows floor. A support statement should be produced by running the build on the
  oldest Windows the project intends to support — which is exactly the clean-VM item above.
- **RAM and disk are not characterised.** The archive reader accepts up to 4 GiB per entry and
  20 GiB total (`internal/infrastructure/archive/reader.go:79-83`) and `MaxArchiveBytes` is 4 GiB
  (`internal/application/backup/ports.go`), and the exporter assembles an archive in memory
  (`readDatabaseBytes`). A very large project therefore has a real memory requirement, and it has not
  been measured.

---

## References

- `docs/ROADMAP.md:573-600` — WP-12 scope, item 9; `:643` — the Release Candidate mapping.
- `docs/SECURITY.md` §1 (keys never committed), §9 (archive limits), §11 (file paths); §19.
- `AGENTS.md` §5 (git and secret safety), §6 (licence and clean-room), §13 (documentation duties).
- `wails.json`; `build/windows/info.json`; `build/windows/wails.exe.manifest`; `main.go:25,79-115`;
  `scripts/verify.sh:144-149`; `.github/workflows/desktop-build.yml:40-70`.
- `internal/infrastructure/appdirs/dirs.go` — where user data lives.
- `internal/domain/provider/provider.go:132` — the credential target namespace.
- `internal/desktop/backup_binding.go:186-264` — the application's existing posture on irreversible
  acts, which an uninstaller should match.
- Wails v2.15.0 (`github.com/wailsapp/wails/v2@v2.15.0`):
  `cmd/wails/flags/build.go:71,120-127` (the `download` default), `internal/wv2installer/wv2installer.go`
  (`MinimumRuntimeVersion`), `pkg/options/windows/windows.go:153-166` (the message set).
- `docs/implementation/RELEASE_BLOCKERS_WP12.md` — the blocker audit, which names "No Wails native
  run" among what it did not verify.
- `docs/adr/0016-encrypted-sensitive-backups-not-in-v1.md` — the sibling WP-12 documentation
  decision.
