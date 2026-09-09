# Security

This is a local, single-owner deployment. Keep it on loopback or behind an SSH
tunnel. A public source repository does **not** mean the running model API is
safe to expose publicly. Do not disable authentication or paired result checks.

Never publish API tokens, SSH credentials, `state/`, uploads, raw conversations,
model weights or unredacted deployment logs. Reference hostnames, paths and
private addresses in the source are configuration examples for the measured
installation, not access credentials.

Local Pi runs with filesystem/network isolation and bounded resources. Only
its paired generation capability is exposed. It can still change the selected
project: review the root, keep a clean branch and inspect diffs. Remote shell
has its SSH account's authority; use a restricted account/container for
untrusted tasks. Local resource limits do not constrain the remote host.

## Reporting a vulnerability

Use this repository's **Security → Report a vulnerability** private channel
when available. Do not post credentials or exploitable deployment details in
a public issue. If private reporting is unavailable, contact the maintainer
through their GitHub profile before sharing sensitive material.

Include the commit, affected component, prerequisites, a minimal sanitized
reproducer and expected/actual behavior. Do not test against either production
rank independently. No response-time or security-audit guarantee is implied.
