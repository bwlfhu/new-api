---
name: remote-debug
description: Inspect and debug the remote new-api Docker Compose deployment on 172.16.22.3. Use when asked to check whether the remote service is up, inspect container health, read logs, test the nginx entrypoint, diagnose relay/API failures, verify ports, or gather status for the remote new-api stack.
---

# Remote Debug

## Overview

Use read-only checks first. The remote stack is live, so avoid restarts, rebuilds, database writes, and volume changes unless the user explicitly asks.

## Remote Facts

Read `references/remote-new-api.md` before debugging. If exact environment values are needed, read `.secrets/remote-new-api.env` locally and keep values out of chat.

## Quick Status

```bash
ssh 172.16.22.3 'docker compose -f /opt/new-api/docker-compose.yml --project-directory /opt/new-api ps'
ssh 172.16.22.3 'docker inspect newapi-app --format "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}"'
curl -fsS -i --max-time 5 http://172.16.22.3:32780/api/status
```

## Logs

Start with short tails and increase only when needed:

```bash
ssh 172.16.22.3 'docker logs --tail 120 newapi-app 2>&1'
ssh 172.16.22.3 'docker logs --tail 80 newapi-nginx 2>&1'
```

For one request, filter by request id if available:

```bash
ssh 172.16.22.3 'docker logs --since 30m newapi-app 2>&1 | grep REQUEST_ID'
```

## Network Checks

```bash
ssh 172.16.22.3 'ss -ltnp | grep -E "(:32780|:3307|:6380|:80)" || true'
ssh 172.16.22.3 'docker network inspect newapi-net --format "{{json .Containers}}"'
ssh 172.16.22.3 'docker exec newapi-nginx wget -qO- http://new-api:3000/api/status | head -c 500'
```

## Config Checks

Inspect config without exposing secrets:

```bash
ssh 172.16.22.3 'cd /opt/new-api && docker compose config --services'
ssh 172.16.22.3 'sed -n "1,220p" /opt/new-api/nginx/conf.d/default.conf'
ssh 172.16.22.3 'docker inspect newapi-app --format "{{json .Mounts}}"'
```

If environment variables are needed, print keys only:

```bash
ssh 172.16.22.3 'docker exec newapi-app env | cut -d= -f1 | sort'
```

## Reporting

Lead with the observed failure or health result, then the exact command evidence. Redact secrets, API keys, cookies, bearer tokens, and full DSNs.
