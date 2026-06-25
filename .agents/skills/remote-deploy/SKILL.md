---
name: remote-deploy
description: Deploy and update this new-api fork on the remote Docker Compose host at 172.16.22.3. Use when asked to publish local changes, rebuild the custom new-api image, restart the remote stack, verify the remote service after deployment, or inspect deployment configuration for the remote new-api Docker setup.
---

# Remote Deploy

## Overview

Deploy through SSH to the remote Docker host. Keep changes explicit, preserve `/opt/new-api/.env` and data directories, and verify the service through nginx after every restart.

## Remote Facts

Read `references/remote-new-api.md` before changing the remote deployment. If the request needs secrets or exact environment values, read local `.secrets/remote-new-api.env`; never paste those values into chat or committed files.

## Deployment Workflow

1. Check the local worktree first:

```bash
git status --short
```

2. Verify the remote stack before touching it:

```bash
ssh 172.16.22.3 'docker compose -f /opt/new-api/docker-compose.yml --project-directory /opt/new-api ps'
curl -fsS http://172.16.22.3:32780/api/status
```

3. Prefer an image-based deployment. Build the app image as `newapi-custom:codex-apikey` only when the requested change is ready and locally verified.

4. Preserve remote state:

- Do not overwrite `/opt/new-api/.env`.
- Do not remove `/opt/new-api/data`, `/opt/new-api/mysql`, or `/opt/new-api/redis`.
- Do not change exposed ports unless the user asks.
- Do not run destructive Docker volume or database commands without explicit user approval.

5. Restart surgically:

```bash
ssh 172.16.22.3 'cd /opt/new-api && docker compose up -d --no-deps new-api'
```

Use full stack restart only when config changes require it:

```bash
ssh 172.16.22.3 'cd /opt/new-api && docker compose up -d'
```

6. Verify:

```bash
ssh 172.16.22.3 'docker inspect newapi-app --format "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}"'
curl -fsS http://172.16.22.3:32780/api/status
ssh 172.16.22.3 'docker logs --tail 80 newapi-app'
```

## Reporting

Report the image/tag deployed, containers restarted, health result, and any logs that matter. Redact secrets and API keys.
