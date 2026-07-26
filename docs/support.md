# Taphaptic Support

## Setup

Install Taphaptic on Apple Watch, run the Mac connector for Claude Code, Codex,
or both, enter the four-digit pairing code, allow notifications, then use
**Settings → Send test notification** on the watch.

## Common fixes

- No Codex alerts: open `/hooks` in Codex, inspect the Taphaptic hooks, and trust
  them. Start a new task after changing hook configuration.
- No Claude Code alerts: start a new Claude Code session after installing hooks.
- Notifications are off: enable Taphaptic notifications in the Watch app on
  iPhone.
- Pairing code expired: run the connector again to generate a new code.
- Hosted relay unavailable: run `taphapticctl health --base-url <relay-url>`.
- Local development discovery failed: keep the Mac and watch on the same LAN
  and restart `./scripts/start-api.sh`.

## Report a problem

Open a public issue for ordinary bugs:
<https://github.com/dzzzgnr/taphaptic/issues/new>.

Use a private security advisory for vulnerabilities or data-deletion requests:
<https://github.com/dzzzgnr/taphaptic/security/advisories/new>.

Do not include API tokens, APNs tokens, prompts, source code, or private logs in
an issue.
