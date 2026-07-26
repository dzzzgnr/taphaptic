# App Store Metadata — Version 1.0

## Identity

- Name: `Taphaptic for Watch`
- Subtitle: `Agent alerts on your wrist`
- Primary category: Developer Tools
- Secondary category: Productivity
- Apple ID: `6794844021`
- Bundle ID: `com.dzzzgnr.taphaptic.watch`
- SKU: `taphaptic-watch-1`
- Version: `1.0.0`
- Minimum OS: watchOS 11
- Distribution: public, free, all 175 App Store countries and regions

## Promotional text

Know when Claude Code or Codex finishes—even when you step away from your Mac.

## Description

Taphaptic brings coding-agent status to Apple Watch.

Connect Claude Code, Codex, or both on your Mac. Taphaptic can alert you when a
main task finishes, a subagent completes background work, or an agent needs
permission or input.

Features:

- direct Apple Watch notifications through APNs;
- Claude Code and Codex lifecycle-hook support;
- one pairing for both providers;
- source-aware notification titles;
- duplicate-event protection;
- a built-in test notification;
- configurable sound and haptic feedback;
- a local/self-hosted mode for development.

Taphaptic sends only minimal lifecycle metadata. It does not send prompts,
transcripts, repository files, tool arguments, source code, or generated
answers.

Taphaptic is an independent product and is not affiliated with, endorsed by, or
sponsored by Anthropic or OpenAI. Claude is a trademark of Anthropic. OpenAI
and Codex are trademarks of OpenAI.

## Keywords

`developer,coding,agent,watch,notification,codex,claude,productivity`

## URLs

- Support: `https://dzzzgnr.github.io/taphaptic/support.html`
- Privacy policy: `https://dzzzgnr.github.io/taphaptic/privacy-policy.html`
- Marketing: `https://dzzzgnr.github.io/taphaptic/`

## App Privacy answers

Declare collection of:

- Device ID — app functionality; not linked to identity; not used for tracking.
- Other Data — minimal agent source/event metadata; app functionality; not
  linked to identity; not used for tracking.

The app does not use data for advertising, analytics, personalization, or
third-party tracking.

## Review notes draft

Taphaptic is a watch-only app. Events originate from lifecycle hooks on a
user-owned Mac and are delivered to the watch through our HTTPS relay and APNs.

Review flow:

1. Launch Taphaptic on Apple Watch.
2. Enter the active four-digit review pairing code supplied in the submission.
3. Allow notifications.
4. Open Settings and tap “Send test notification.”
5. Put the app in the background to confirm standard watchOS notification
   delivery.

The review installation and relay must stay active throughout review. No paid
account or purchase is required.

## Required assets

- 1–10 Apple Watch screenshots using one currently accepted App Store Connect
  size consistently.
- Screens to capture: pairing, pending/connected, Claude completion, Codex
  permission request, notification settings/test confirmation.
- App icon is generated from `watch-app/taphaptic.icon`.
- Four 416 x 496 Series 11 simulator captures from `release/screenshots/` are
  uploaded and processed successfully in App Store Connect.
