# saymer-alt WDTT source maintenance

Base: hoaxisr/proxy-turn-vk-android, awg-client,
`1110a75f76e42c23149b8e24f9182c1ff179e515`.
GPLv3 LICENSE and upstream authorship/notices are preserved. This fork does not
change third-party distribution terms. No new license audit is asserted.

Ported from saymer-alt/entware-go PR10 commit
`c8e362d32069b1edc11f6762872e4e423a4b8813`:

| Original patch/overlay | Effective source | Purpose |
|---|---|---|
| 010-laboratory-security.patch, security.go/security_test.go | go_client, lab_security*.go | Private JSON/credential files, no secret argv/output, exclusive loopback UDP, bounded workers, disabled diagnostics and AWG daemon initialization |
| 020-server-auth-lab.patch, server_auth_test.go | hardened-relay/server.go, server_auth_test.go | Require AUTH before forwarding; reject invalid/absent AUTH and unbound device |
| 030-server-policy.patch, server_policy*.go | hardened-relay/server.go, server_policy*.go | Expiry/device binding and active-session revocation |
| server_entry.go, server_pair_fixture_test.go | hardened-relay | Relay-only private-file entry, no upstream API/Telegram/TUN/NAT/firewall setup; loopback TURN/WG test harness |

The three archived patches are provenance, already applied to the effective
sources. Do not apply them again. The original root server.go is preserved for
upstream history and negative AUTH baseline tests. The maintained standalone
relay is **hardened-relay**, not the root upstream executable. Its module lock is
the unchanged pinned client module graph, matching the Phase4 harness.

PHASE4-SOURCE-EQUIVALENCE.json records every effective Go/module file SHA256.
No runtime code was redesigned during this port. Entware IPK/CI remains in
entware-go; Keenetic lifecycle/init remains in KAS PR125 at
`7c01e29c0ef278532e083a0d6868cfef4d2ca003`.

## Build an exact revision

Use the existing verified Entware SDK Go1.26.1, GOTOOLCHAIN=local, CGO_ENABLED=0.
Do not let Go download a newer toolchain. From a verified clean detached checkout:

```sh
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOTELEMETRY=off CGO_ENABLED=0
cd go_client
GOOS=linux GOARCH=arm64 /absolute/pinned/go build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /private/wt-client-arm64 .
GOOS=linux GOARCH=mipsle GOMIPS=softfloat /absolute/pinned/go build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /private/wt-client-mipsle .
cd ../hardened-relay
GOOS=linux GOARCH=amd64 /absolute/pinned/go build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /private/wdtt-relay .
```

Build commands do not start a connection or relay. Physical/VK acceptance is not
inferred from local fixtures. No binary upload/release workflow is added here.

## Upstream sync

Keep awg-client/awg-server as upstream-tracking branches; use a separate feature
branch for modifications. Fetch read-only first:

```sh
git remote add upstream https://github.com/hoaxisr/proxy-turn-vk-android.git
git fetch upstream --tags
git switch awg-client
git merge --ff-only upstream/awg-client
git push origin awg-client
```

Add upstream only if absent. Stop on divergence; never force-push. For integration
into the maintained feature branch, prepare a separate PR and rerun source-lock,
security, ARM64/MIPSel, opkg/lifecycle and local network acceptance. No automatic
merge or release. Back up all refs before integration; retain older bundles.
