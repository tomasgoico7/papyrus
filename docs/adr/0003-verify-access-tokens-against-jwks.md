# 0003. Verify access tokens against JWKS

- **Status:** accepted
- **Date:** 2026-09-18 (recorded retroactively; decided during the initial build)

## Context

Supabase used to sign access tokens with HS256 and a shared secret copied into
each backend. Newer projects sign with asymmetric keys — ES256 — and publish the
public half at a JWKS endpoint derived from the project URL.

Verifying a modern token the old way fails with a 401 that looks like a broken
key rather than a wrong verification method, which is exactly how this surfaced.

Two further concerns shaped the implementation. Keys rotate, and a gateway that
caches them forever starts rejecting valid tokens until it is redeployed. And a
verifier that accepts whatever algorithm the token header names is open to
algorithm confusion, where an attacker re-signs with a symmetric algorithm using
the public key as the secret.

## Decision

The gateway fetches the project JWKS, caches it, and resolves the verification
key by the token's `kid`. An unknown `kid` triggers a refresh, subject to a
minimum interval so a stream of bad tokens cannot be turned into a request
amplifier against the JWKS endpoint.

Accepted algorithms are pinned to `ES256` and `RS256`. The JWK parsing is written
against the Go standard library rather than pulling in a JWKS dependency.

## Consequences

No shared secret exists anywhere in the stack — the only Supabase value the
gateway needs is the project URL, which is not secret. Key rotation is handled
without a redeploy.

The costs: roughly a hundred lines of key parsing to own and test, and a startup
dependency on an endpoint that must be reachable. The first request after a
rotation pays a fetch.

Writing the parsing by hand was a deliberate choice for a project whose purpose is
learning. A team optimising for maintenance should take a maintained JWKS library
instead.
