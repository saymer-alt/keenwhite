# Contributing to KeenWhite

This is a new independent GitHub repository, not a fork. All original first-party transport tooling is developed here. The WDTT implementation is an intentional GPL-3.0 source migration, imported with full provenance and independent from the MIT core. Other third-party source requires separate license review.

1. Keep KAS and KeenWhite responsibilities separate.
2. Run `go vet ./...`, `go test -race ./...` and `make crossbuild` before a PR.
3. Reject unsupported states and unsupported network binds; never infer E2E readiness from a listener or config file.
4. New providers require explicit `wire` and `server_class`, source and licence provenance, an admission policy, isolated tests, and real hardware acceptance before enabling.
5. Keep secrets and real user information out of code, logs, fixtures, and issues. Sample inputs must be synthetic.
6. No install, server deployment, release or routing change without explicit owner approval.
7. WDTT GPL source migration must keep original copyright, LICENSE and Git history; do not portray upstream contributors as KeenWhite authors. New dependencies and copied code need a license audit; PolyForm and unreviewed GPL code is not allowed into the MIT core.

The roadmap in `docs/ROADMAP.md` is a plan, not evidence that any adapter works.
