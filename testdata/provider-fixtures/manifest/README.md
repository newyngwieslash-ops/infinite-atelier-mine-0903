# Provider Manifest fixtures (RP-05.1)

Original fixtures authored for this repository — no third-party protocol
document is copied here. Each `negative-*` file differs from
`base-valid.json` by EXACTLY ONE property, so a validation rule's refusal is
attributable: a negative case that changed two things proves nothing.

- `base-valid.json` — the minimal document that decodes and validates.
- `negative-*.json` — one-property mutations, each refused by a named rule.

These files are read by `manifest_rp05_test.go` in the domain package.
