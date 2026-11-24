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
- `backend`: REST API сервис на Go (вебхуки MAX и ЮKassa, FSM-бот, Magic Link / OTP / JWT / API Token авторизация, ЛК, CRM, планировщик напоминаний, чанковая загрузка аудио, генерация PDF-выписок).
- `worker`: Фоновый Go-воркер Asynq — скачивает мастер-PDF из MinIO, наносит именные кириллические водяные знаки и колонтитулы (`DejaVuSans` + `pdfcpu`), рассылает email-уведомления.
- `ml-worker`: Python-воркер — нормализует аудио (`ffmpeg` 16кГц моно WAV), выполняет транскрибацию (`mock` или `faster_whisper`), маскирует ПДн (152-ФЗ) и формирует черновик клинического саммари (`mock` / `yandexgpt` / `gigachat`) для врача.
- `frontend-cabinet`: Веб-кабинет пациентки (Vue 3 + Vite SPA: вход по Magic Link / Email OTP, дашборд, модули, статус водяных знаков и скачивание PDF по pre-signed URL, 3 режима консультаций, анализы, опубликованные заключения, продление доступа).
- `frontend-admin`: Панель врача и CRM (Vue 3 + Vite SPA: аналитика, очереди, пациентки и подписки, заказы и ручное одобрение канала MAX, загрузка мастер-PDF и тарифов, ответы на вопросы из бота, интерактивный плеер с кликабельной стенограммой Whisper и утверждение рекомендаций Human-in-the-Loop).
- `clients/desktop-recorder`: Исходный код Windows-клиента на C# (.NET 8 WPF + NAudio) для захвата системного звука Телемоста/VK (WASAPI Loopback) + микрофона с локальным кэшем и чанковой отправкой.
- `nginx`: Корневой реверс-прокси, маршрутизирующий `/cabinet/`, `/admin/`, `/api/`, `/webhooks/`, `/s3/` и поддерживающий загрузку аудиофайлов размером до 500 МБ.

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
- `make test-ml` — Юнит-тесты Python для `ml-worker` (маскирование ПДн, аудио-конвейер, мок STT/LLM).
- `make smoke` — Проверка `/healthz` запущенного стенда (через Nginx или напрямую на порт 8080).
- `make clean` — Остановить сервисы и очистить тома данных (volumes).

## Доступ и режимы разработки

- По умолчанию в `.env.example` включены безопасные режимы `MAX_MODE=mock`, `YOO_MODE=mock`, `EMAIL_MODE=mock`, `STT_PROVIDER=mock`, `LLM_PROVIDER=mock`.
- Учётная запись врача/администратора для входа в `/admin/`: `ADMIN_EMAIL` / `ADMIN_PASSWORD` (по умолчанию `doctor@zdorovie.local` / `zdorovie_doctor_pass`), а `DOCTOR_API_TOKEN` используется Windows-рекордером.
- Для тестовой оплаты без реальной кассы ЮKassa используется встроенный локальный экран подтверждения `/api/v1/dev/mock-pay`. Для переключения на реальные API задайте `MAX_MODE=live`, `YOO_MODE=live`, `STT_PROVIDER=faster_whisper`, `LLM_PROVIDER=yandexgpt` (или `gigachat`) и соответствующие ключи в `.env`.

---

## Локальное тестирование MAX-бота и вебхуков (MAX и ЮKassa)

В локальном окружении (когда `PUBLIC_BASE_URL` без `https://`, например `http://localhost`) при `MAX_MODE=live` и заданном `MAX_BOT_TOKEN` сервис `backend` автоматически запускает long-polling `GET /updates` к `https://platform-api.max.ru` (туннель для MAX не обязателен).

Для приёма вебхуков от `platform-api.max.ru` (при деплое или проверке webhook-режима) и ЮKassa на локальный стенд используйте HTTPS-туннель (например, [ngrok](https://ngrok.com/) или localtunnel):

```bash
ngrok http 80
```

Полученный публичный адрес (например, `https://abcdef.ngrok-free.app`) укажите при регистрации подписки на вебхук:
- MAX Bot API: `https://abcdef.ngrok-free.app/webhooks/max` (заголовок `X-Max-Bot-Api-Secret`)
- ЮKassa: `https://abcdef.ngrok-free.app/webhooks/yukassa`
