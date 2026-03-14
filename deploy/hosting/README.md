# Hosting Setup (Watch-Only Flow)

This folder documents how to publish the onboarding installer asset:

- `https://agentwatch.app/install/claude` (installer shell script)

## 1) Generate static hosting assets

Run from repo root:

```sh
./deploy/prepare-hosting-assets.sh
```

Optional legacy iPhone universal-link assets:

```sh
AGENTWATCH_ENABLE_LEGACY_AASA=1 \
AGENTWATCH_APPLE_TEAM_ID="YOUR_TEAM_ID" \
AGENTWATCH_IOS_BUNDLE_ID="local.agentwatch.phone"
./deploy/prepare-hosting-assets.sh
```

Generated files:

- `deploy/web/agentwatch.app/install/claude`
- legacy only: `deploy/web/pair.agentwatch.app/...apple-app-site-association`

## 2) Serve with correct content types

- `/install/claude` should be served as plain text shell script.
- legacy only: `apple-app-site-association` must be served as `application/json` with no redirects.

## 3) Vercel deployment (recommended)

Prepare + deploy installer project:

```sh
sh ./deploy/vercel/deploy-vercel.sh
```

Then map domains in Vercel:

- `agentwatch.app` -> project rooted at `deploy/web/agentwatch.app`
- legacy only: `pair.agentwatch.app` -> project rooted at `deploy/web/pair.agentwatch.app`

## 4) Nginx example (alternative)

```nginx
server {
  listen 443 ssl http2;
  server_name agentwatch.app;

  location = /install/claude {
    root /var/www/agentwatch/deploy/web/agentwatch.app;
    default_type text/plain;
    add_header Cache-Control "no-cache";
  }
}

# Optional legacy iPhone universal-link host.
server {
  listen 443 ssl http2;
  server_name pair.agentwatch.app;

  location = /.well-known/apple-app-site-association {
    root /var/www/agentwatch/deploy/web/pair.agentwatch.app;
    default_type application/json;
    add_header Cache-Control "no-cache";
  }

  location = /apple-app-site-association {
    root /var/www/agentwatch/deploy/web/pair.agentwatch.app;
    default_type application/json;
    add_header Cache-Control "no-cache";
  }
}
```

## 5) Smoke checks

```sh
curl -i https://agentwatch.app/install/claude
# Optional legacy check:
# curl -i https://pair.agentwatch.app/.well-known/apple-app-site-association
```
