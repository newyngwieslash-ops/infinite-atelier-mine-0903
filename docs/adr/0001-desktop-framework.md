# ADR-0001 Wails v2 as the Desktop Framework

- Status: Accepted
- Date: 2026-09-04
- Deciders: Product specification owners; repository engineering
- Related work package: WP-00 (decision record), WP-01 (implementation)

## Context

Infinite Atelier is currently a React/Vite browser application. The approved product becomes a single-process, local-first desktop application with a Go Core, SQLite metadata, managed local files, secure OS-backed secrets, durable jobs/workflows, and typed frontend bindings. Existing React UI and infinite-canvas capability must be retained.

The repository currently has no desktop framework, Go module, native bindings, or packaging configuration. The PRD fixes the MVP desktop line to stable Wails v2 and explicitly excludes Wails v3 beta from the release-critical foundation.

## Decision drivers

- Reuse the existing React/TypeScript UI rather than rewrite it.
- Make Go the in-process authority for domain, persistence, files, providers, jobs, agents, workflows, memory, and backup.
- Keep the MVP a local single-process application rather than introduce services.
- Support Windows, macOS, and Linux with a small, auditable IPC surface.
- Keep secrets and privileged primitives out of the webview.
- Prefer a stable release line for the MVP and defer framework-generation risk.

## Options considered

### Wails v2

Embeds the existing web frontend and exposes Go application methods/events. It aligns directly with the approved architecture and minimizes frontend replacement. Costs include binding lifecycle discipline, platform webview differences, and Wails-specific packaging/tooling.

### Electron

Would preserve web compatibility and has a mature ecosystem, but introduces a Node/Chromium runtime and a second privileged JavaScript boundary. That works against the selected Go Core/security model and normally increases package/runtime footprint.

### Tauri

Offers a compact webview desktop shell, but makes Rust the native framework boundary while the approved core is Go. Combining a Tauri/Rust shell with a Go sidecar/library adds process/FFI, packaging, cancellation, and security complexity without a product requirement.

### Browser/PWA only

Matches the current code but cannot provide the required OS SecretStore, controlled filesystem, desktop packaging, and trusted single-process Go boundary.

## Decision

Use the stable Wails v2 line for the desktop MVP. The React/Vite application remains the presentation layer. Wails bindings expose narrow application DTOs and commands; they do not expose repositories, SQL, arbitrary filesystem/network operations, or secrets.

The dependency direction is:

```text
React binding client
        ↓
Desktop/Wails adapters
        ↓
Application ports
        ↓
Domain

Infrastructure implements ports and never becomes a frontend API surface.
```

Wails events may report durable state changes, but the database read model remains authoritative. Cancellation uses explicit application commands/context propagation, not webview-only flags. Frontend DTOs may contain secret references/status, never raw secret values.

WP-00 records this decision only. WP-01 is responsible for selecting the exact v2 version from the stable line, introducing the module/shell, and proving platform/toolchain behavior; it must not silently move to v3.

## Consequences

### Positive

- Existing React routes/components can migrate incrementally.
- Go owns privileged operations in process.
- The architecture avoids an additional Node or service runtime in production.
- Typed bindings provide an explicit replacement seam for browser stores/services.

### Negative / risks

- Webview differences and native prerequisites require a platform matrix.
- Generated bindings need a committed/generated-artifact policy.
- Long-running operations must not block the UI binding thread.
- Development-mode Vite behavior must not leak into packaged production security assumptions.
- Wails v2 upgrades and eventual v3 migration require separate ADRs.

## Verification

WP-01 must demonstrate:

1. a minimal Wails v2 shell on the development host;
2. React build embedded and served without exposing `0.0.0.0`;
3. typed health/version binding and event/cancellation smoke path;
4. clean separation of Domain/Application/Ports/Infrastructure/Desktop packages;
5. Windows build plus documented macOS/Linux CI or matrix plan;
6. no raw secret, SQL, filesystem path primitive, or arbitrary network binding exposed to React.

## References

- `PRD.md` desktop and fixed product decisions
- `docs/ARCHITECTURE.md`
- `docs/SECURITY.md`
- `docs/ROADMAP.md` WP-01
- `docs/implementation/BASELINE.md`
