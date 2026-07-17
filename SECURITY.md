# Security policy

## Supported versions

Vericopy is in pre-release development. Security fixes are applied to the latest
tagged minor release and the default branch. A support table will be added before
`1.0.0` when multiple release lines exist.

## Report a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security advisory reporting for this repository. If private reporting is not
enabled, contact the maintainer through the verified contact method on their
GitHub profile and include only enough detail to establish a secure channel.

Include:

- affected version and platform;
- the diagnostic code and sanitized output;
- reproduction steps using disposable hosts and keys;
- impact and attacker prerequisites;
- whether host keys, path parsing, partial state, permissions, or output
  redaction are involved.

Never include private keys, passwords, tokens, production hostnames, or personal
file paths. You should receive an acknowledgment within seven days. Disclosure
timing will be coordinated after impact is understood and a fix is available.

## Scope notes

The [security model](docs/security-model.md) states the product guarantees and
non-goals. A remote administrator changing a file after finalization, a malicious
source file, and exact Windows ACL replication are outside the initial guarantee.

