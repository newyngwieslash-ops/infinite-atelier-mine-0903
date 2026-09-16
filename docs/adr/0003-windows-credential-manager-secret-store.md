# ADR-0003 Windows Credential Manager as the WP-02 Secret Backend

- Status: Accepted (WP-02 scope)
- Date: 2026-09-15
- Deciders: Repository engineering under approved WP-02 plan
- Related work package: WP-02 (Secret, network security and Provider Gateway foundation)

## Context

The PRD fixes that API keys live in the OS credential store, that the database
holds only secret references, and that the frontend never receives a raw key
(PRD §5.14/§5.15, FR-140, `docs/SECURITY.md` §4). WP-02 is the first package
that must store a secret at all, so it must choose a concrete backend for the
first-shipped platform (Windows) and define fail-closed behavior elsewhere.

Constraints from the approved plan and the repository state:

- Windows is the first shipped platform and the only platform WP-02 proves.
- `go.mod` does not contain a keyring library, and the user's approved plan
  selected Windows Credential Manager with no new supply-chain surface.
- `golang.org/x/sys/windows` is already an indirect dependency and provides
  lazy DLL loading, `GetLastError`, and UTF-16 conversion.
- No plaintext fallback is permitted: a missing backend must disable external
  providers rather than degrade to a file, database, or browser store.

## Decision

Implement a Windows-only adapter over `advapi32.dll` in
`internal/infrastructure/secretstore` using `CredReadW`, `CredWriteW`,
`CredDeleteW`, and `CredFree` through `windows.NewLazySystemDLL`. Secrets are
stored as `CRED_TYPE_GENERIC` credentials with `CRED_PERSIST_LOCAL_MACHINE`
under the target namespace `InfiniteAtelier:provider:<provider-id>`.

On every non-Windows platform the same port is implemented by
`UnavailableStore`, which reports `Available() == false` and fails closed on
write/resolve/delete. `main` selects the implementation at compile time
(`platform_secretstore_windows.go` / `_other.go`), so no runtime branch can
silently pick a weaker backend.

Scope boundaries:

- The value is passed as a byte slice, copied into the credential blob, and the
  caller's slice is zeroed after use by the adapter that consumed it. The store
  does not cache values.
- `Resolve` exists on the port because provider adapters must build the
  Authorization header in Go. It is never bound to Wails; the desktop binding
  exposes only `Status`, `Set`, and `Delete`.
- Deleting a missing credential is not an error (`ERROR_NOT_FOUND` maps to a
  no-op), so replace/delete flows are idempotent.
- Provider IDs are constrained to `[a-z0-9-]{1,64}` so a caller cannot forge a
  credential target belonging to another application or path.

## Alternatives considered

| Option | Why not selected |
|---|---|
| DPAPI (`CryptProtectData`) with a file or DB blob | Stores ciphertext at rest, not a managed credential; the PRD/SECURITY text names the OS credential store/keyring, and DPAPI blobs invite the plaintext-adjacent storage this package must avoid. |
| Third-party keyring library | Adds a new dependency and supply-chain surface for one platform; the approved plan explicitly chose a small in-repo adapter. |
| Plaintext or obfuscated config/DB storage | Forbidden by PRD §5.13/§5.14 and SECURITY §4.1 (no silent plaintext fallback). |
| An encrypted local vault in WP-02 | SECURITY §4.1 permits it only as a separate user-enabled feature with its own ADR; out of WP-02 scope. |
| Fail-closed on Windows too (no native backend) | Would make the first shipped platform unable to use any provider, contradicting WP-02's goal of a working first adapter. |

## Consequences

Positive:

- No new module dependency; the surface is four documented Win32 calls.
- The reference never contains the secret, so the SQLite schema and ordinary
  backups can be verified to exclude key material by construction.
- Non-Windows behavior is explicit and testable (`wincred_other_test.go`).

Negative / risks:

- The adapter is `unsafe`-adjacent Win32 interop: `CREDENTIALW` field order and
  error handling must be correct. This is covered by a live round-trip test
  against the real credential manager using owned, uniquely prefixed targets.
- macOS (Keychain) and Linux (Secret Service) backends are not implemented;
  those platforms run with providers disabled until a later package.
- Credential Manager applies per-user, per-machine protection based on the OS
  profile; it is not an encrypted backup mechanism and is not treated as one.

## Verification

- `TestWindowsStorePutResolveDelete`, `TestWindowsStoreOverwriteReplacesValue`,
  `TestWindowsStoreRejectsInvalidInput`, and
  `TestWindowsStoreMissingResolveFailsClosed` run against the real Windows
  Credential Manager and only create/delete entries under their own random test
  prefix (`wincred_windows_test.go`).
- `TestUnavailableStoreFailsClosed` proves the non-Windows contract; it
  cross-compiles and runs on any non-Windows host.
- `internal/application/secrets` tests prove the service never returns a value,
  reports fail-closed status when the store is unavailable, and never leaks a
  backend error into a user-visible message.
- `internal/desktop/provider_bindings_test.go` proves the binding surface has
  no resolve method and fails closed when unattached.
- `scripts/security-scan.mjs` scans for high-confidence key patterns and
  unguarded config serialization in CI.

## References

- `PRD.md` fixed decisions 12–13 and FR-140
- `docs/SECURITY.md` §4 (Secret management) and §17 (security error codes)
- `docs/ARCHITECTURE.md` §15 (Secret Store contract)
- `docs/ROADMAP.md` WP-02
- `docs/ACCEPTANCE.md` AC-FOUND-004
