.PHONY: help up down restart logs ps init-db test smoke clean

help:
	@echo "Команды для управления локальным стендом проекта «Больше чем здоровье»:"
	@echo "  make up       - Собрать и запустить все сервисы стенда в фоновом режиме"
	@echo "  make down     - Остановить все сервисы стенда"
	@echo "  make restart  - Перезапустить контейнеры стенда"
	@echo "  make logs     - Просмотр агрегированных логов сервисов в реальном времени"
	@echo "  make ps       - Статус контейнеров и состояние healthcheck проверок"
	@echo "  make init-db  - Повторно применить DDL-схему БД PostgreSQL"
	@echo "  make test     - Юнит-тесты Go для backend и worker (стенд не нужен)"
	@echo "  make smoke    - Проверка эндпоинта /healthz запущенного стенда"
	@echo "  make clean    - Остановить контейнеры и удалить тома данных (volumes)"

up:
	@if [ ! -f .env ]; then cp .env.example .env; echo ".env создан из .env.example"; fi
	docker compose up -d --build

down:
	docker compose down

restart:
	docker compose restart

logs:
	docker compose logs -f

ps:
	docker compose ps

init-db:
	docker compose exec -T postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < deploy/postgres/init.sql

test:
	@echo "Запуск юнит-тестов Go (backend и worker)..."
	docker run --rm -v $$(pwd)/services/backend:/app -w /app golang:1.27.4-alpine go test -v ./...
	docker run --rm -v $$(pwd)/services/worker:/app -w /app golang:1.27.4-alpine go test -v ./...

smoke:
	@echo "Проверка эндпоинта здоровья запущенного стенда..."
	@curl -sf http://localhost/healthz || curl -sf http://localhost:8080/healthz
	@printf "\nСтенд отвечает!\n"

clean:
	docker compose down -v
