# Remote new-api deployment facts

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

- `newapi-app`: image `newapi-custom:codex-apikey`, service `new-api`, internal port `3000`, healthcheck probes `/api/status`
- `newapi-nginx`: image `nginx:alpine`, service `nginx`, host port `32780` -> container `80`
- `newapi-mysql`: image `mysql:8.0`, host port `3307` -> container `3306`
- `newapi-redis`: image `redis:7-alpine`, host port `6380` -> container `6379`

## Remote files

- `/opt/new-api/.env`
- `/opt/new-api/docker-compose.yml`
- `/opt/new-api/nginx/conf.d/default.conf`
- `/opt/new-api/data`
- `/opt/new-api/mysql`
- `/opt/new-api/redis`

Local private copies live under `.secrets/` at the project root.
