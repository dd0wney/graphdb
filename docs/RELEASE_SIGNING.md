# Verifying GraphDB releases

GraphDB release artifacts on GitHub — `graphdb_<version>_<os>_<arch>.tar.gz` and
`checksums.txt` — are signed with the GraphDB release GPG key. Each artifact has
a detached, ASCII-armored signature (`.asc`) attached to the release.

## Signing key

| | |
|---|---|
| Algorithm | ed25519 |
| UID | `GraphDB Release <noreply@graphdb.io>` |
| Fingerprint | `7E6B 6544 1BFF CB61 AE8C  77D5 E7C5 13EB 926B 660B` |

The public key lives in [`KEYS`](../KEYS) at the repository root and is attached
to each release as `graphdb-release-pubkey.asc`.

## Verify a download

```bash
# 1. Import the key once (from the repo checkout)
gpg --import KEYS
# ...or straight from a release asset:
#   gpg --import graphdb-release-pubkey.asc

# 2. Verify a tarball against its signature
gpg --verify graphdb_0.8.0_Linux_x86_64.tar.gz.asc graphdb_0.8.0_Linux_x86_64.tar.gz

# 3. (optional) Verify the checksums file, then confirm the tarball's hash
gpg --verify checksums.txt.asc checksums.txt
sha256sum -c checksums.txt 2>/dev/null | grep graphdb_0.8.0_Linux_x86_64
```

A good signature prints:

```
gpg: Good signature from "GraphDB Release <noreply@graphdb.io>"
```

GnuPG will also warn that the key is not certified with a trusted signature —
that is expected for a release key you have not personally signed. Confirm the
**fingerprint** above matches before trusting the artifact.
