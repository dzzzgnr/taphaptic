# Physical Watch Validation Checklist

Use this checklist before release tagging to confirm the real device flow still works.

Target duration on a known-good machine: **under 15 minutes**.

## Preconditions

- macOS with Xcode installed and Apple signing/team configured.
- Physical Apple Watch paired to iPhone.
- Mac and watch on the same local network.
- Repo is up to date and you are testing the exact commit planned for release.
- `./scripts/doctor.sh` passes.
- If testing an unreleased branch, set `TAPHAPTIC_DEV_MODE=1` and make sure Go is installed.

## Validation Steps

1. Start from a clean local runtime state:

```sh
./scripts/stop-api.sh
```

Expected: API is stopped (or already stopped).

2. Run bootstrap:

```sh
./scripts/bootstrap-watch.sh
```

Expected:
- preflight passes,
- API starts (or reports already running),
- pairing code is printed,
- Xcode project opens.

3. Deploy to physical watch in Xcode:
- Scheme: `Taphaptic`
- Destination: your physical Apple Watch
- Press **Run**

Expected: app installs/launches on watch without build/signing errors.

4. Pair watch with printed code:
- Open Taphaptic on watch.
- Enter the 4-digit code from bootstrap output.

Expected: pairing completes and app becomes ready for events.

5. Verify event delivery:

```sh
./scripts/test-claude-connection.sh stop
```

Expected:
- command exits successfully,
- watch receives the event/haptic notification.

6. Verify API lifecycle regression quickly:

```sh
./scripts/stop-api.sh
./scripts/start-api.sh
./scripts/test-claude-connection.sh stop
```

Expected:
- stop/start works cleanly,
- event still reaches watch after restart.

## Recovery / Cleanup

If validation fails:

1. Restart API and retry event:

```sh
./scripts/stop-api.sh
./scripts/start-api.sh
./scripts/test-claude-connection.sh stop
```

2. Re-run onboarding to regenerate pairing and hooks:

```sh
./scripts/connect-claude-code.sh --scope user
```

3. Check API logs:

```sh
tail -n 80 "$HOME/Library/Logs/Taphaptic/api.log"
```

After validation, optional cleanup:

```sh
./scripts/stop-api.sh
```

## Release Readiness Template

Copy and paste into release PR notes:

```md
## Physical Watch Regression Result

- Date:
- Tester:
- Commit:
- Machine:
- watchOS version:
- iOS version:
- `TAPHAPTIC_DEV_MODE` value:

### Checklist
- [ ] Preconditions passed
- [ ] Bootstrap passed
- [ ] Xcode deploy to physical watch passed
- [ ] Pairing with code passed
- [ ] `./scripts/test-claude-connection.sh stop` reached watch
- [ ] API stop/start regression passed

### Overall
- PASS / FAIL:
- Notes:
```
