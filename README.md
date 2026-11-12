# Больше чем здоровье — Беременность без тревоги

Платформа интенсива **«Беременность без тревоги: понятный путь к здоровой беременности»**.
Автор — практикующий врач-терапевт, нефролог, нутрициолог и IV-терапевт.

Платформа включает:
- Чат-бот в мессенджере MAX с FSM-анкетой сбора данных и выставлением счетов;
- Платежный контур ЮKassa с фискализацией НПД («Мой налог»);
- Личный веб-кабинет пациентки для изучения материалов и отслеживания консультаций;
- Панель управления врача и CRM;
- Хранилище MinIO (S3) с защищенными бакетами и pre-signed ссылками;
- Фоновый воркер генерации персонализированных водяных знаков на PDF-материалах курса;
- ML-воркер транскрибации аудиозаписей консультаций (Whisper / ffmpeg) и формирования саммари.

---

## Требования к окружению

- **Docker** 24.0+ / 26.0+
- **Docker Compose** v2.20+
- Утилиты `make`, `curl`

---

## Быстрый запуск стенда

1. Склонируйте репозиторий и перейдите в каталог проекта:
   ```bash
   cd zdorovie
   ```

2. Скопируйте шаблон переменных окружения:
   ```bash
   cp .env.example .env
   ```

3. Соберите и запустите сервисы стенда:
   ```bash
   make up
   ```
   *(или напрямую через `docker compose up -d --build`)*

4. Проверьте статус контейнеров и прохождение healthcheck-проверок:
   ```bash
   make ps
   ```

---

## Карта портов и эндпоинтов стенда

| Сервис | Внутренний порт | Внешний URL / Порт | Назначение |
|---|---|---|---|
| **Nginx Reverse Proxy** | 80 | `http://localhost/` | Точка входа в платформу |
| **Личный веб-кабинет** | 80 | `http://localhost/cabinet/` | SPA кабинет пациентки |
| **Панель управления / CRM** | 80 | `http://localhost/admin/` | SPA интерфейс врача и CRM |
| **Backend REST API** | 8080 | `http://localhost/api/` (`http://localhost:8080`) | REST API бэкенда |
| **Эндпоинт здоровья** | 8080 | `http://localhost/healthz` | Проверка статуса БД и Redis |
| **Вебхук MAX бота** | 8080 | `http://localhost/webhooks/max` | Вебхук событий мессенджера MAX |
| **Вебхук ЮKassa** | 8080 | `http://localhost/webhooks/yukassa` | Вебхук нотификаций платежей |
| **MinIO S3 API** | 9000 | `http://localhost/s3/` (`http://localhost:9000`) | S3 API для файлов |
| **MinIO Web Console** | 9001 | `http://localhost:9001` | Веб-консоль управления S3 |
| **PostgreSQL 16** | 5432 | `localhost:5432` | Реляционная БД |
| **Redis 7** | 6379 | `localhost:6379` | Кэш и брокер очередей Asynq |

---

## Состав сервисов стенда

- `postgres`: База данных PostgreSQL 16 с персистентным томом `postgres_data` и автонакатом DDL-схемы из `deploy/postgres/init.sql` (таблицы тарифов, пользователей, заказов, оплат, подписок, материалов, слотов консультаций, транскриптов и саммари).
- `redis`: Брокер сообщений Redis 7 с томом `redis_data` для очередей задач Asynq (`watermark`, `audio_transcribe`, `text_summarize`, `notifications`).
- `minio`: S3-хранилище с томом `minio_data`.
- `minio-init`: Одноразовый контейнер на базе `minio/mc`, автоматически создающий приватные бакеты `zdorovie-master`, `zdorovie-client-materials`, `zdorovie-consultations`.
- `backend`: REST API сервис на Go (вебхуки MAX и ЮKassa, проверка здоровья, FSM).
- `worker`: Фоновый Go-воркер обработки задач генерации защитных водяных знаков на PDF.
- `ml-worker`: Python-воркер транскрибации аудиозаписей встреч с `ffmpeg` и интеграцией со структурированием саммари.
- `frontend-cabinet`: Веб-кабинет пациентки (Nginx-контейнер со статическим SPA-каркасом).
- `frontend-admin`: Панель врача/CRM (Nginx-контейнер со статическим SPA-каркасом).
- `nginx`: Корневой реверс-прокси, маршрутизирующий запросы и поддерживающий загрузку аудиофайлов размером до 500 МБ.

---

## Проверка работоспособности

1. **Проверка эндпоинта здоровья бэкенда**:
   ```bash
   curl -i http://localhost/healthz
   ```
   Ожидаемый ответ: `HTTP 200 OK`
   ```json
   {"status":"ok","db":"connected","redis":"connected","timestamp":"..."}
   ```

2. **Проверка инициализации БД и тарифов**:
   ```bash
   docker compose exec postgres psql -U zdorovie_user -d zdorovie_db -c "SELECT id, code, title, duration_days FROM tariffs;"
   ```

3. **Проверка инициализации бакетов MinIO**:
   `minio-init` — одноразовый контейнер: он создаёт бакеты, выводит их список и завершается с кодом 0, поэтому `docker compose exec minio-init` не сработает. Список бакетов смотрите в его логе или запросите отдельным запуском клиента `mc`:
   ```bash
   docker compose logs minio-init
   docker compose run --rm --entrypoint sh minio-init -c 'mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc ls local/'
   ```
   *(или через веб-консоль MinIO по адресу http://localhost:9001, логин `minio_admin` / пароль `minio_secret_key`)*

---

## Доступные команды Makefile

- `make up` — Собрать и запустить все контейнеры стенда в фоне.
- `make down` — Остановить и удалить контейнеры стенда.
- `make restart` — Перезапустить все сервисы.
- `make logs` — Просмотр объединенных логов всех сервисов.
- `make ps` — Просмотр состояния сервисов и healthcheck.
- `make init-db` — Повторно применить DDL-схему к базе данных.
- `make test` — Юнит-тесты Go для backend и worker в контейнере `golang`; поднятый стенд не нужен.
- `make smoke` — Проверка `/healthz` запущенного стенда (через Nginx или напрямую на порт 8080).
- `make clean` — Остановить сервисы и очистить тома данных (volumes).

---

## Локальное тестирование вебхуков MAX и ЮKassa

Для получения вебхуков от `platform-api2.max.ru` и ЮKassa на локальный стенд используйте HTTPS-туннель (например, [ngrok](https://ngrok.com/) или localtunnel):

```bash
ngrok http 80
```

Полученный публичный адрес (например, `https://abcdef.ngrok-free.app`) укажите при регистрации подписки на вебхук:
- MAX Bot API: `https://abcdef.ngrok-free.app/webhooks/max` (заголовок `X-Max-Bot-Api-Secret`)
- ЮKassa: `https://abcdef.ngrok-free.app/webhooks/yukassa`
