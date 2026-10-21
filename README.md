# zhim-back

Backend интернет-магазина zhim.kg: Go API + Telegram-бот, контракт API и инфраструктура.
Фронтенд (витрина и админка) — в соседнем репозитории [zhim-front](https://github.com/Baiel1922/zhim-front).

```
zhim-back/
  backend/   Go API + Telegram-бот (chi, pgx/sqlc, goose, oapi-codegen)
  api/       openapi.yaml — единый контракт API (source of truth)
  deploy/    docker-compose.*.yml, позже Caddyfile и бэкапы
  Makefile   все основные команды
```

## Локальный запуск

Оба репозитория клонируются рядом:

```bash
git clone https://github.com/Baiel1922/zhim-back.git
git clone https://github.com/Baiel1922/zhim-front.git
cd zhim-back
cp .env.example .env   # заполнить секреты: openssl rand -hex 32
make dev
```

Если zhim-front лежит в другом месте: `make dev FRONT_DIR=/path/to/zhim-front`.

| Сервис        | URL                            |
| ------------- | ------------------------------ |
| API           | http://localhost:8090/healthz  |
| Витрина       | http://localhost:3000          |
| Админка       | http://localhost:5173          |
| imgproxy      | http://localhost:8081          |
| MinIO console | http://localhost:9001          |
| PostgreSQL    | localhost:5432                 |

Порты меняются в `.env` (`API_PORT`, `WEB_PORT`, …).

## Команды

`make help` — список. Основные: `make dev`, `make up` / `make down`, `make gen`, `make test`, `make lint`,
`make migrate`, `make migrate-new name=...`, `make seed`.

Порядок изменений API: сначала `api/openapi.yaml`, потом `make gen`, потом код.
