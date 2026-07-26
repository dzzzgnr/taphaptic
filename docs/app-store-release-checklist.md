# Taphaptic 1.0 Release Checklist

## Accounts and service

- [ ] Apple Developer agreements are current.
- [x] Bundle ID `com.dzzzgnr.taphaptic.watch` exists with Push Notifications.
- [x] Team-scoped sandbox and production APNs key `FJ4N6FV4DG` exists.
- [x] Apple Distribution certificate and App Store profile exist for the bundle
      ID, with the private key installed in the local Keychain.
- [x] Dedicated App Store Connect team key `Z4Z562KGS3` is stored outside the
      repository for provisioning and upload.
- [ ] Production APNs key is mounted from the relay secret manager.
- [ ] Production HTTPS relay has persistent encrypted storage, backups, health
      monitoring, and alerting.
- [ ] Render Blueprint is synced from `render.yaml`; APNs values marked
      `sync: false` are entered only in the Render secret UI.
- [ ] Production relay URL is set as `TAPHAPTIC_API_BASE_URL` for the archive.
- [ ] A stable review installation and pairing code are ready.

## Automated gates

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/smoke-local-e2e.sh
./scripts/test-shared-swift.sh
./scripts/build-watch-app.sh
```

- [ ] Release archive succeeds with Xcode 26+ and watchOS 26+ SDK.
- [ ] Archive contains `aps-environment=production`.
- [ ] Archive contains `PrivacyInfo.xcprivacy`.
- [ ] Organizer/App Store Connect validation succeeds.

## Physical watch gates

- [ ] Clean install and pairing.
- [ ] Claude-only, Codex-only, and combined setups.
- [ ] Main completion, subagent completion, and permission request.
- [ ] One duplicate hook event produces one notification.
- [ ] Alert arrives with app closed and display asleep.
- [ ] Test notification works.
- [ ] Notification denial and later re-enable are understandable.
- [ ] Reset/uninstall prevents future notifications.
- [ ] Wi-Fi, iPhone-proxied, and cellular/off-LAN paths.

## App Store Connect

- [x] App record `Taphaptic for Watch` exists (Apple ID `6794844021`).
- [x] Version metadata and independence disclaimer entered.
- [x] Privacy/support URLs are publicly reachable.
- [x] App Privacy matches `docs/app-store-metadata.md` and is published.
- [x] Age rating completed (4+).
- [x] Free pricing and worldwide availability are configured.
- [x] Public distribution is selected; Mac and Vision Pro availability is off.
- [ ] Export compliance confirms only exempt system HTTPS/APNs cryptography.
- [x] Four Apple Watch Series 11 screenshots uploaded and processed.
- [ ] Review notes include a live code and exact test steps.
- [ ] Internal TestFlight pass completed on a clean watch.
- [ ] External TestFlight feedback resolved.
- [x] Manual release selected for version 1.0.
