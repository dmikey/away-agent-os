# Away cloud control plane — operator preview

Away can use the Akash Console API for cloud infrastructure without giving users a blockchain wallet, SSH keys, or an Akash credential. The native Mac app connects to an Away control plane; that service owns the infrastructure credential and authenticates each account independently.

**This is infrastructure management, not a released cloud agent runtime.** Planning, sign-in, deployment creation, provider bidding/lease acceptance, closure and restart reconciliation are implemented. Agent execution, workspace uploads, remote terminal access and model credential delivery remain disabled. Leased infrastructure is explicitly shown as **Worker verification required**. There is no fallback to an ordinary unverified container.

No public Away cloud endpoint is included. Local Away remains usable without cloud configuration. There are no sample cloud credentials, fake deployments, or background deployments.

## Operator setup

1. Create a dedicated Akash project and a project-scoped Console API key. Configure project budget alerts and stop-top-ups in Console. The API key and project UUID must be supplied to the control-plane process, outside the agent workspace:

   - `DEREK_AKASH_KEY`: secret Console credential, injected by the operator's secret manager.
   - `AWAY_AKASH_PROJECT_ID`: project UUID.

   Do not put either credential in SDL, source control, screenshots, shell history, a worker image or the native UI. The service never returns the key or upstream error bodies. The fixed upstream is `https://console-api.akash.network` and redirects are not followed.

2. Validate credentials without creating resources:

   ```sh
   go run ./cmd/away-cloud -check-akash
   ```

   This only requests one existing deployment record. A successful check proves API/project access; it does not prove billing readiness, provider capacity or TEE support.

3. Register an OIDC **native/public** client. Use authorization-code flow with PKCE S256 and refresh-token rotation. The provider must support RFC 8252 loopback redirect URIs: `http://127.0.0.1:<ephemeral-port>/callback`. No client secret belongs in the Mac app. Configure:

   - `AWAY_OIDC_ISSUER`: HTTPS issuer URL.
   - `AWAY_OIDC_CLIENT_ID`: the native client's audience/client ID.
   - `AWAY_CLOUD_ALLOWED_SUBJECTS`: comma-separated admitted account subject IDs.

   All discovery/auth/token/JWKS endpoints must use HTTPS. The native client verifies signature, issuer, audience, expiry, nonce and state before saving a session. The service verifies the signed ID token and the operator allowlist on every protected request. Records are scoped by issuer and subject; switching identity providers does not transfer ownership. The ID token serves as the bearer credential for this specific native application, not as an arbitrary third-party access token. Use a dedicated OIDC client for Away.

4. Run the service on a trusted operator machine with a persistent private disk. The database must be outside all agent-visible folders, backed up and protected by host access controls. It stores infrastructure records and audit events, not API tokens or agent files.

   ```sh
   go run ./cmd/away-cloud -data /var/lib/away/cloud.db
   ```

   It binds only to `127.0.0.1:8789`. Place a TLS reverse proxy in front with request limits and rate limits. Do not expose an unencrypted port. Health/config discovery is public; all account/infrastructure routes require a verified account token. Cookie auth and browser-origin requests are rejected. No CORS grant is supplied. Keep this control plane outside provider-controlled agent containers.

5. In Away OS, open the local workspace status → **Cloud**, enter the operator's HTTPS service origin and choose **Sign in securely**. Authentication opens the system browser, not Away's agent browser. Tokens and refresh tokens stay in macOS Keychain. Sign-out clears the saved session on that Mac; it does not stop a deployment or revoke the identity-provider grant. Remove an admitted subject and restart the service to revoke its control-plane access; also revoke its refresh-token grant at the identity provider.

## Infrastructure review and launch

Provisioning is **off by default**. First publish and review a public worker image, then pin `AWAY_CLOUD_WORKER_IMAGE` to `registry/repository@sha256:<64 lowercase hex characters>`. Mutable tags and manifest injection are rejected. No worker image is supplied by this preview because the remote execution isolation/attestation boundary is not yet complete.

The native UI can prepare a plan without spending infrastructure credits. Plans request 2 CPUs, 4 GiB RAM, 10 GiB disk, one CPU confidential VM and a 1–24 hour runtime. The manifest has no user/provider secrets, host mounts, public SSH service or automatic web previews. Its maximum placement bid is 1000 uact per block. This is a chain-denominated limit, **not a USD budget**.

Only an operator intentionally testing infrastructure should set `AWAY_CLOUD_ALLOW_PROVISIONING=true`. Startup then checks the configured project credential. End users must approve the exact saved plan, then separately accept a current provider quote before leasing. Each account can have one unresolved/active workspace. **Do not enable this for general users until remote execution and attestation are complete:** a lease in this preview consumes compute credits while agent execution remains locked.

Akash automatically tops up deployments from account credits. Automatic funding cannot be disabled. Runtime limits close a deployment after the limit measured from lease start; these limits are not a complete monetary cap. Project budgets, funding settings, chain costs, storage persistence and backups need separate operator review. Closing a deployment may delete its storage. Away waits for a read-back confirming chain state `closed` before permitting another workspace.

## Recovery and reconnecting

The bbolt transaction store saves approval and intent **before** an infrastructure call. Native client disconnection does not cancel an already approved deployment request. Each request has a finite service-owned timeout. Repeating a launch reopens its existing record, without issuing another create request.

If a mutation times out, the service crashes, or persistence fails after an upstream request, the record becomes `reconcile`. No automatic paid retry occurs. Known deployment IDs can be refreshed in the native panel. An unknown creation outcome blocks another plan until an operator compares the Console deployment list with the approved manifest and identifies the exact deployment.

With the service stopped, and after manually matching that deployment in Console, use the same environment and database:

```sh
go run ./cmd/away-cloud -data /var/lib/away/cloud.db \
  -reconcile-workspace RECORD_ID \
  -reconcile-subject OIDC_SUBJECT \
  -reconcile-dseq DEPLOYMENT_SEQUENCE
```

This reads the project-scoped deployment, binds it to that existing record and records the operator decision. It never creates infrastructure. The operator is responsible for matching the approved resource manifest; the deployment sequence alone is not proof of that match. Existing deployment IDs cannot be silently replaced. For an absent/failed creation with no chain record, leave the account blocked until the operator has resolved the outcome; do not edit the database to bypass the guard.

Provider lease failures can mean either “no lease created” or “lease exists but manifest not delivered.” Away treats both as reconciliation and does not blindly accept another bid. Refresh the existing deployment or close it in Console before proceeding.

The durable records in this preview describe **infrastructure lifecycle**, not durable agent turns. Existing local agent work still depends on the Mac app. Do not advertise “agents keep working while your Mac is off” yet.

## Remaining runtime release gates

Before dispatching any cloud agent work:

- Build a pinned, unprivileged worker image with mandatory sandbox capability probes; no host shell, public SSH, broad host mounts or unsandboxed browser fallback.
- Verify fresh AMD SEV-SNP/Intel TDX attestation against trusted vendor roots and expected measurements; bind the attested worker/workload identity and TLS key to the broker connection. A TEE SDL flag or provider label is insufficient.
- Implement worker registration and authenticated, narrowly scoped task dispatch; don't place bootstrap bearer secrets in public manifests.
- Persist conversation/checkpoint/tool results with replay-safe recovery, approvals, cancellation and ownership checks. Resume after disconnect without duplicating external actions.
- Keep provider credentials in an operator vault and broker model calls; do not place them in worker environment files. Persist workspace files encrypted outside ephemeral provider storage.
- Enforce network permissions through a non-bypassable egress boundary inside the verified worker, and prove revocation, tenant separation, shutdown and backup recovery on actual Akash infrastructure.

These are real release gates, not implemented claims. Akash Confidential Compute is currently experimental.

## Validation

```sh
go test -race ./internal/cloud ./cmd/away-cloud
```

Fixtures also run the native browser callback and token exchange against a local TLS identity provider, including PKCE, forged state rejection, nonce rejection, refresh, UI token redaction and logout. Fixtures cover tenant separation, real signed OIDC token verification (issuer/audience/expiry/account), default launch denial, stale approvals and quotes, concurrent/replayed creates, observer disconnection, restart recovery, fixed API contracts, manifest injection and secret redaction. These are local tests; no Akash deployment is created.

Primary references: [Console API](https://akash.network/docs/api-documentation/console-api/api-reference/), [organizations and projects](https://akash.network/docs/api-documentation/console-api/organizations-and-projects/), [experimental confidential compute](https://akash.network/docs/learn/core-concepts/confidential-compute/), [native OAuth guidance](https://www.rfc-editor.org/rfc/rfc8252).
