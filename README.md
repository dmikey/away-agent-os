# Away OS — macOS

A shared productivity desktop for working **with** agents: rooms, conversations, code, notebooks, drafts, files and visible work. React runs in a native Wails window; Go owns local persistence and the execution boundary.

Away OS runs locally on **Apple Silicon macOS**, with a shared workspace for you and your agents.

## First launch

Away welcomes new workspaces with a four-step setup: choose OpenAI, Anthropic, or xAI; follow the provider’s key link and connect an API key; name your first agent and select an available model; then choose a working style and refine their instructions. Keys are saved in macOS Keychain, separate from workspace files. API usage is billed by your provider.

Setup opens only Messenger, directly in a conversation with your new agent. Other apps are available from the Dock. Setup starts for a new workspace or when no provider key is saved. Existing files, messages, and agents are preserved; returning users can cancel the assistant to keep working offline. To run it again, open **Chat settings → Connection → Setup assistant…**. This adds a provider/agent without deleting existing work.

## What works

- ICQ-inspired messenger, agent definitions, rooms, and Keychain-backed provider connections.
- Interactive xterm.js terminal backed by a real Linux PTY: persistent shell state, streamed output, interactive input, Ctrl+C, history, and resize. Close the terminal to export changes to the virtual filesystem; concurrent editor edits are preserved and conflicts recovered. Reconnect to load new editor changes.
- Draggable/resizable windows, active-app menus, Dock, boot screen, and calendar.
- Shared virtual filesystem and Explorer, local-file import, Code, Writer, scratchpad and `.ipynb` editing.
- Python files and notebook cells execute inside a dedicated Linux VM for each actor. An actor’s code and notebooks share Python variables; other actors have separate kernels.
- A guest Linux terminal, exposed to agents as `sandbox_shell`.
- `web_fetch` returns bounded readable text through the Go gateway. HTML scripts/styles are removed. HTTPS is terminated for this explicit fetch, without intercepting other guest connections.
- Inspectable tool catalog with search, descriptions and parameters. Built-in agent tools use the same validated commands as the UI. A local external MCP server is not implemented yet; front-end WebMCP depends on WebView support.

## Execution and network boundary

macOS uses Apple’s Virtualization.framework directly. Firecracker requires Linux KVM; it is not the active macOS backend. There is no nested virtualization dependency on M1/M2.

Each actor receives a 512 MB, two-vCPU Linux VM; at most three VMs are active. The filesystem boundary is the complete `/workspace`: virtual workspace files plus folders explicitly shared by the user under `/workspace/Shared`. Approved folders use live Apple virtiofs mounts with per-folder read-only or read/write enforcement. No host block disks, SSH agents, native process handles or network adapters are attached. Bounded workspace/result messages, approved virtiofs shares, and the controlled HTTP proxy are the communication paths.

The guest runs Python and shell commands as UID 1000. Code is not passed to a host shell or interpreter. The host enforces a watchdog and stops VMs on cancellation or shutdown. There is no host execution fallback. Use **Local runtime → Isolation → Reset all VMs** to discard guest kernel variables.

Networking starts with an empty per-actor allowlist on each app launch. In **Local runtime → Network**, approve exact public domains. IP addresses, wildcards, private/link-local/metadata destinations, and the Mac’s own addresses are blocked. DNS is resolved and validated by Go, and the connection is pinned to the validated IP. Changing a policy cancels active requests for that actor.

The proxy permits HTTP on port 80 and HTTPS CONNECT on port 443. Limits are 60 requests/minute, four connections per actor, a 60-second connection lifetime, 1 MB HTTP request bodies, 4 MB HTTP responses and 4 MB per TLS direction. HTTPS tunnel contents stay encrypted. The in-memory activity log holds 200 destination/decision/status/byte records; headers, queries, bodies and API keys are omitted. Explicit `web_fetch` requests have a 30-second limit, do not follow redirects, and trim returned text to at most 50,000 characters.

External web pages are not embedded in the privileged native WebView. They appear as escaped readable text through the controlled fetch tool; a human can open HTTPS links in the system browser. The built-in `away://` pages are bundled local assets.

## Share a folder

Open **Explorer → Shared folders → Share folder…**, choose a folder with the macOS picker, name its mount, and select read-only or read/write access. It appears at `/workspace/Shared/<name>` for every agent, Terminal, Code, and Python notebook. Read/write changes affect the selected Mac folder directly. The rest of the Mac filesystem and native processes remain outside this grant.

Changing or removing a grant restarts sandbox VMs; finish active runs and close/save Terminal first. Files on the Mac are kept when a folder is unshared. Folder grants persist in owner-only local configuration, outside agent state. A replaced/missing directory does not silently inherit an old grant. Native editor operations use a confined `os.Root` bridge; agents cannot add folder grants or provide arbitrary host paths.

Mounted data is live and does not use the 2.4 MB virtual-file snapshot quota. The current UI lists folders with up to 500 entries and opens files up to 384 KB; larger files remain accessible from the VM. Move/delete operations on mounted files use Terminal or Finder. Virtual workspace imports/exports skip mount points, so cell refreshes and terminal saves do not traverse or delete real shared folders.

## Build

Requirements: Apple Silicon, macOS 13+, Xcode Command Line Tools, Go 1.27+, Python 3, Node 22.13+ and pnpm 11.25.0. Install the pinned Wails CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
./scripts/build.sh
```

The build downloads only pinned Linux/Alpine/Python artifacts listed in `sandbox/macos-guest-lock.json`, verifies their SHA-256 hashes, cross-compiles the Linux-only guest agent, and assembles a reproducible initramfs. Guest programs are never executed on the Mac during the build. `AWAY_GUEST_SOURCE_CACHE` can select an existing verified source cache.

The package script generates Finder/Dock/Retina icons from `build/appicon.png`, builds outside iCloud-synced Documents folders, signs with the `com.apple.security.virtualization` entitlement, verifies the signature, and writes `build/bin/Away OS-macOS-arm64.zip`. It prefers a single installed Developer ID Application certificate, using hardened runtime and a secure timestamp. Set `AWAY_SIGN_IDENTITY` to select a certificate when several are installed, or `-` for an ad-hoc development build. Without a Developer ID certificate it falls back to ad-hoc signing. It prints the staged `.app` path. Signing does **not** notarize the app; notarization remains a separate release step.

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

Set `AWAY_SHARES_SMOKE_TEST=1` and `AWAY_DATA_DIR` to a temporary directory when launching the signed app to verify live virtiofs reads/writes, read-only enforcement, large-file access, sibling Python imports, mount survival across workspace refreshes, shared access from another actor, revocation, and the full virtual filesystem boundary. This uses disposable fixture folders only. The signed CLI runs the same check with `AWAY_SHARE_CHECK=1 /tmp/away-vmcheck`.

Set `AWAY_TERMINAL_SMOKE_TEST=1` and `AWAY_DATA_DIR` to a temporary directory when launching the signed app to exercise the real guest PTY: TTY identity, UID 1000, shell state, interactive stdin, resize, Ctrl+C, workspace export, and host isolation. Test instances use a separate instance lock and do not interrupt a running workspace. VM validation failures include the macOS diagnostic; unsigned executables cannot use virtualization.

Set `AWAY_SMOKE_TEST=1` and `AWAY_DATA_DIR` to a temporary directory when launching the staged app binary for a boot check. It exits only after React has completed the real workspace boot and acknowledged the Go bridge. It never prints workspace contents or keys.

## Data and limits

Local state is stored at `~/Library/Application Support/Away OS/workspace.json`, with owner-only permissions and atomic revision-checked saves. It is plaintext JSON, not application-level encrypted storage. Provider keys are stored in macOS Keychain. OpenAI, Anthropic, and xAI calls use fixed Go-mediated endpoints; keys do not enter the VM or workspace file. Google Colab is not enabled in the macOS build.

The inherited virtual filesystem currently supports 256 entries and 2.4 MB encoded storage. Individual exports are bounded; failed/oversized exports do not infer deletions. Concurrent edits preserve newer files and recover guest versions under `/workspace/Conflicts/<run-id>`. Guest disks are ephemeral: only validated, synchronized virtual workspace files persist. Initial guest Python includes the standard library; installing arbitrary native packages is not implemented.

## Status and security scope

This is a working development prototype, not a completed security audit. Passing boundary tests does not prove protection against every hypervisor, kernel, WebView or application vulnerability. Source review, dependency maintenance, signed distribution, stronger audit persistence, sandbox lifecycle polish, and independent security testing remain before production use with sensitive data. A task board, canvas, Git UI, media suite, shared audio and full browser automation are still planned; the tool catalog does not claim those exist.

## Third-party software

Application code is MIT-licensed. Vendored Code-Hex/vz retains its upstream MIT license and a documented Objective-C class-name compatibility patch. Alpine packages, Linux and standalone CPython retain their respective licenses. Source artifacts and corresponding source/build recipes are identified in `sandbox/SOURCES.md`. Generated guest images and binary release archives are excluded from Git.
