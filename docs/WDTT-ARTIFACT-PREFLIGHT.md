# WDTT binary artifact preflight (offline)

**Status:** independent KeenWhite first-party code, opt-in preflight. No WDTT process, VPN/turn session, router configuration, KAS installer or external network call is involved.

## What the check does

`keenwhite verify-wdtt` takes a locally supplied *client* binary and an **independently obtained trusted SHA256** (for example, the published checksum from an explicitly approved, pinned, reproducible CI or release source). It:

1. Rejects missing/nonregular/symlink/empty/over-256MiB artifacts; does not follow the last-component symlink.
2. Streams and compares the full SHA256; rejects a mismatch.
3. Parses the ELF header *without running the binary*. Requires Linux `arm64` (ELF64 AArch64 little-endian) or `mipsle` (ELF32 MIPS little-endian), executable/PIE type.
4. Emits `HASH_AND_ARCH_MATCH` with explicit `tcp_e2e=NOT_TESTED`, `udp_e2e=NOT_TESTED`. Never labels a binary or tunnel production ready.

## Usage

```sh
go run ./cmd/keenwhite verify-wdtt \
  -file /secure/location/wt-client-linux-arm64 \
  -sha256 <TRUSTED_SHA256_FROM_SEPARATE_PROVENANCE> \
  -arch arm64 -json
```

For MIPSel use `-arch mipsle`, an actual MIPSel ELF artifact, and **its own** checked SHA256. The two architecture digests differ.

**Do not** derive the expected SHA by hashing the very same untrusted file being checked: that defeats provenance verification. A hash/ELF match alone **does not** attest the producer or signer's identity. Check the source commit, trusted release/CI origin and signatures separately.

## Fail-closed boundaries

A mismatch is an error. No executable is ever run. No secrets/links are read, no port is opened, no manifest state is promoted to `BUILD_CONFIRMED` or `E2E_VERIFIED`. This is not an installer admission guarantee for a mutable path: files can change after inspection and symlinked parent directories are not independently sealed. Future safe install must atomically stage a pinned copy to an owned immutable/private directory, re-hash at use time, verify process identity and run physical E2E checks.

Existing WDTT GPL source remains inside `transports/wdtt/`; this standalone inspector uses Go standard library only and belongs to the original MIT KeenWhite core.

References: [WDTT migration CI acceptance](migration/WDTT-CI-ACCEPTANCE.md), [KeenWhite WDTT source](../transports/wdtt/), [KW-M2 #3](https://github.com/saymer-alt/keenwhite/issues/3).
