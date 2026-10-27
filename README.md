# Away OS — macOS

A shared productivity desktop for working **with** agents: rooms, conversations, code, notebooks, drafts, files and visible work. React runs in a native Wails window; Go owns local persistence and the execution boundary.

Away OS runs locally on **Apple Silicon macOS**, with a shared workspace for you and your agents.

## What works

- ICQ-inspired messenger, agent definitions, rooms, and per-session provider keys.
- Draggable/resizable windows, active-app menus, Dock, boot screen, and calendar.
- Shared virtual filesystem and Explorer, local-file import, Code, Writer, scratchpad and `.ipynb` editing.
- Python files and notebook cells execute inside a dedicated Linux VM for each actor. An actor’s code and notebooks share Python variables; other actors have separate kernels.
- A guest Linux terminal, exposed to agents as `sandbox_shell`.
- `web_fetch` returns bounded readable text through the Go gateway. HTML scripts/styles are removed. HTTPS is terminated for this explicit fetch, without intercepting other guest connections.
- Inspectable tool catalog with search, descriptions and parameters. Built-in agent tools use the same validated commands as the UI. A local external MCP server is not implemented yet; front-end WebMCP depends on WebView support.

## Execution and network boundary

macOS uses Apple’s Virtualization.framework directly. Firecracker requires Linux KVM; it is not the active macOS backend. There is no nested virtualization dependency on M1/M2.

Each actor receives a 512 MB, two-vCPU Linux VM; at most three VMs are active. No host folders, host disks, SSH agents, Docker sockets, credentials, native process handles or network adapters are attached. The only communication paths are bounded workspace/result messages and a controlled HTTP proxy over virtio-vsock.

The guest runs Python and shell commands as UID 1000. Code is not passed to a host shell or interpreter. The host enforces a watchdog and stops VMs on cancellation or shutdown. There is no host execution fallback. Use **Local runtime → Isolation → Reset all VMs** to discard guest kernel variables.

Networking starts with an empty per-actor allowlist on each app launch. In **Local runtime → Network**, approve exact public domains. IP addresses, wildcards, private/link-local/metadata destinations, and the Mac’s own addresses are blocked. DNS is resolved and validated by Go, and the connection is pinned to the validated IP. Changing a policy cancels active requests for that actor.

The proxy permits HTTP on port 80 and HTTPS CONNECT on port 443. Limits are 60 requests/minute, four connections per actor, a 60-second connection lifetime, 1 MB HTTP request bodies, 4 MB HTTP responses and 4 MB per TLS direction. HTTPS tunnel contents stay encrypted. The in-memory activity log holds 200 destination/decision/status/byte records; headers, queries, bodies and API keys are omitted. Explicit `web_fetch` requests have a 30-second limit, do not follow redirects, and trim returned text to at most 50,000 characters.

External web pages are not embedded in the privileged native WebView. They appear as escaped readable text through the controlled fetch tool; a human can open HTTPS links in the system browser. The built-in `away://` pages are bundled local assets.

## Build

Requirements: Apple Silicon, macOS 13+, Xcode Command Line Tools, Go 1.27+, Python 3, Node 22.13+ and pnpm 11.25.0. Install the pinned Wails CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
./scripts/build.sh
```

The build downloads only pinned Linux/Alpine/Python artifacts listed in `sandbox/macos-guest-lock.json`, verifies their SHA-256 hashes, cross-compiles the Linux-only guest agent, and assembles a reproducible initramfs. Guest programs are never executed on the Mac during the build. `AWAY_GUEST_SOURCE_CACHE` can select an existing verified source cache.

The package script builds outside iCloud-synced Documents folders, ad-hoc signs with the `com.apple.security.virtualization` entitlement, verifies the signature, and writes `build/bin/Away OS-macOS-arm64.zip`. It prints the staged `.app` path. This is a local development package; it is **not notarized** or a production release.

## Verify

```sh
(cd frontend && pnpm run check)
node scripts/check-transport.mjs
go test -race ./...
go build -o /tmp/away-vmcheck ./cmd/vmcheck
codesign --force --sign - --entitlements build/darwin-entitlements.plist /tmp/away-vmcheck
/tmp/away-vmcheck
```

The VM check boots real Linux guests and verifies Linux identity, inaccessible/unchanged host canaries, workspace export, persistent Python variables, separate actor kernels, cancellation, default-deny networking, one explicitly approved `example.com` HTTPS request, separate actor policies and recorded activity. All check VMs are stopped afterward. No provider API calls are part of these tests.

Set `AWAY_SMOKE_TEST=1` and `AWAY_DATA_DIR` to a temporary directory when launching the staged app binary for a boot check. It exits only after React has completed the real workspace boot and acknowledged the Go bridge. It never prints workspace contents or keys.

## Data and limits

Local state is stored at `~/Library/Application Support/Away OS/workspace.json`, with owner-only permissions and atomic revision-checked saves. It is plaintext JSON, not application-level encrypted storage. API keys stay in the current UI session and provider calls use a fixed Go-mediated OpenAI endpoint; keys do not enter the VM or workspace file. Google Colab is not enabled in the macOS build.

The inherited virtual filesystem currently supports 256 entries and 2.4 MB encoded storage. Individual exports are bounded; failed/oversized exports do not infer deletions. Concurrent edits preserve newer files and recover guest versions under `/workspace/Conflicts/<run-id>`. Guest disks are ephemeral: only validated, synchronized virtual workspace files persist. Initial guest Python includes the standard library; installing arbitrary native packages is not implemented.

## Status and security scope

This is a working development prototype, not a completed security audit. Passing boundary tests does not prove protection against every hypervisor, kernel, WebView or application vulnerability. Source review, dependency maintenance, signed distribution, stronger audit persistence, sandbox lifecycle polish, and independent security testing remain before production use with sensitive data. A task board, canvas, Git UI, media suite, shared audio and full browser automation are still planned; the tool catalog does not claim those exist.

## Third-party software

Application code is MIT-licensed. Vendored Code-Hex/vz retains its upstream MIT license and a documented Objective-C class-name compatibility patch. Alpine packages, Linux and standalone CPython retain their respective licenses. Source artifacts and corresponding source/build recipes are identified in `sandbox/SOURCES.md`. Generated guest images and binary release archives are excluded from Git.
