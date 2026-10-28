# Sandbox implementation

The active macOS backend is `internal/macvm/`: one direct Apple VZ Linux VM per actor, with no network adapter and only explicit, human-approved folder mounts under /workspace/Shared. All other host paths and native process controls are excluded. The guest broker is `cmd/guest/`; the default-deny Go gateway is `internal/gateway/`. See the root README for verified behavior and current limitations.

Firecracker is reserved for a future Linux backend. The optional pinned engine lock and fetch script download inert release archives only; they do not constitute a running Firecracker sandbox. No privileged daemon or control VM is installed on the Mac.
