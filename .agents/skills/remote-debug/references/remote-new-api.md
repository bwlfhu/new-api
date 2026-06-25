# Remote new-api debug facts

- SSH target: `huyuwen@172.16.22.3`
- Hostname: `dev.remote`
- Runtime: Docker Compose
- Compose project: `new-api`
- Compose file: `/opt/new-api/docker-compose.yml`
- Working directory: `/opt/new-api`
- Public URL: `http://172.16.22.3:32780`
- Health URL: `http://172.16.22.3:32780/api/status`
- Docker network: `newapi-net`

## Containers

- `newapi-app`: image `newapi-custom:codex-apikey`, status running and healthy when probed on 2026-06-10
- `newapi-nginx`: image `nginx:alpine`, host port `32780` -> container `80`
- `newapi-mysql`: image `mysql:8.0`, host port `3307` -> container `3306`
- `newapi-redis`: image `redis:7-alpine`, host port `6380` -> container `6379`

## Nginx

`/opt/new-api/nginx/conf.d/default.conf` proxies all paths to Docker DNS upstream `new-api:3000`, disables proxy buffering, and sets 3600s read/send timeouts.

## Useful status commands

```bash
ssh 172.16.22.3 'docker compose -f /opt/new-api/docker-compose.yml --project-directory /opt/new-api ps'
curl -fsS -i http://172.16.22.3:32780/api/status
ssh 172.16.22.3 'docker logs --tail 120 newapi-app 2>&1'
ssh 172.16.22.3 'docker logs --tail 80 newapi-nginx 2>&1'
```

Local private copies of remote config live under `.secrets/` at the project root.
