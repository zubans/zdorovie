-- Database Initialization Script
-- Platform: Беременность без тревоги

-- 1. Тарифные планы
CREATE TABLE IF NOT EXISTS tariffs (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) UNIQUE NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    price_kopecks BIGINT NOT NULL,
    duration_days INT NOT NULL,
    has_feedback BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 2. Пользователи платформы
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    max_user_id VARCHAR(128) UNIQUE,
    max_username VARCHAR(128),
    full_name VARCHAR(255) NOT NULL,
    phone VARCHAR(32) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    pregnancy_status VARCHAR(128),
    role VARCHAR(32) DEFAULT 'client',
    password_hash TEXT,
    api_token_hash VARCHAR(128) UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS max_username VARCHAR(128);
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS api_token_hash VARCHAR(128);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_api_token_hash ON users(api_token_hash) WHERE api_token_hash IS NOT NULL;

-- 3. Заказы
CREATE TABLE IF NOT EXISTS orders (
    id BIGSERIAL PRIMARY KEY,
    order_number VARCHAR(64) UNIQUE NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    tariff_id INT NOT NULL REFERENCES tariffs(id) ON DELETE RESTRICT,
    amount_kopecks BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    consent_accepted_at TIMESTAMP WITH TIME ZONE,
    paid_at TIMESTAMP WITH TIME ZONE,
    channel_access_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    channel_approved_at TIMESTAMP WITH TIME ZONE,
    channel_joined_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS consent_accepted_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS paid_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS channel_access_status VARCHAR(32) NOT NULL DEFAULT 'pending';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS channel_approved_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS channel_joined_at TIMESTAMP WITH TIME ZONE;

-- 4. Платежи ЮKassa
CREATE TABLE IF NOT EXISTS payments (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    yukassa_payment_id VARCHAR(128) UNIQUE NOT NULL,
    status VARCHAR(64) NOT NULL,
    amount_kopecks BIGINT NOT NULL,
    is_npd_registered BOOLEAN DEFAULT TRUE,
    receipt_url TEXT,
    confirmation_url TEXT,
    expires_at TIMESTAMP WITH TIME ZONE,
    raw_payload JSONB,
    paid_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE payments ADD COLUMN IF NOT EXISTS confirmation_url TEXT;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP WITH TIME ZONE;

-- 5. Подписки и сроки доступа
CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    tariff_id INT NOT NULL REFERENCES tariffs(id) ON DELETE RESTRICT,
    starts_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    reminder_14d_sent_at TIMESTAMP WITH TIME ZONE,
    reminder_7d_sent_at TIMESTAMP WITH TIME ZONE,
    reminder_1d_sent_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS reminder_14d_sent_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS reminder_7d_sent_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS reminder_1d_sent_at TIMESTAMP WITH TIME ZONE;
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_active ON subscriptions(user_id, is_active, expires_at);

-- 6. Эталонные материалы (Master)
CREATE TABLE IF NOT EXISTS materials (
    id SERIAL PRIMARY KEY,
    module_number INT NOT NULL,
    lesson_number INT NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    content_markdown TEXT,
    video_url TEXT,
    duration_minutes INT NOT NULL DEFAULT 0,
    file_type VARCHAR(32) NOT NULL,
    s3_master_key VARCHAR(512) NOT NULL,
    is_published BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE materials ADD COLUMN IF NOT EXISTS content_markdown TEXT;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS video_url TEXT;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS duration_minutes INT NOT NULL DEFAULT 0;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
CREATE UNIQUE INDEX IF NOT EXISTS idx_materials_module_lesson_type ON materials(module_number, lesson_number, file_type, title);

-- 6a. Прогресс прохождения материалов пациенткой
CREATE TABLE IF NOT EXISTS user_material_progress (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    material_id INT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    completed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, material_id)
);

-- 7. Персонализированные файлы клиентов (Watermarked)
CREATE TABLE IF NOT EXISTS client_materials (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    material_id INT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    s3_client_key VARCHAR(512) NOT NULL,
    watermark_signature TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    generated_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, material_id)
);

-- 8. Расписание и слоты консультаций
CREATE TABLE IF NOT EXISTS consultation_slots (
    id BIGSERIAL PRIMARY KEY,
    doctor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
    slot_start TIMESTAMP WITH TIME ZONE NOT NULL,
    slot_end TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'available',
    meeting_url TEXT,
    client_question TEXT,
    doctor_notes TEXT,
    reminder_24h_sent_at TIMESTAMP WITH TIME ZONE,
    reminder_2h_sent_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE consultation_slots ADD COLUMN IF NOT EXISTS reminder_24h_sent_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE consultation_slots ADD COLUMN IF NOT EXISTS reminder_2h_sent_at TIMESTAMP WITH TIME ZONE;

-- 8a. Прикрепленные файлы анализов перед консультацией
CREATE TABLE IF NOT EXISTS consultation_attachments (
    id BIGSERIAL PRIMARY KEY,
    slot_id BIGINT NOT NULL REFERENCES consultation_slots(id) ON DELETE CASCADE,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_name VARCHAR(255) NOT NULL,
    content_type VARCHAR(128) NOT NULL,
    file_size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(512) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 9. Аудиозаписи консультаций
CREATE TABLE IF NOT EXISTS consultation_records (
    id BIGSERIAL PRIMARY KEY,
    slot_id BIGINT NOT NULL REFERENCES consultation_slots(id) ON DELETE CASCADE,
    doctor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_type VARCHAR(32) NOT NULL,
    original_file_name VARCHAR(255) NOT NULL,
    s3_audio_key VARCHAR(512) NOT NULL,
    file_size_bytes BIGINT NOT NULL,
    duration_seconds INT,
    audio_format VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'uploaded',
    error_message TEXT,
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_consultation_records_slot ON consultation_records(slot_id);
CREATE INDEX IF NOT EXISTS idx_consultation_records_client ON consultation_records(client_id);

-- 10. Посегментный транскрипт встречи (Whisper STT)
CREATE TABLE IF NOT EXISTS consultation_transcripts (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES consultation_records(id) ON DELETE CASCADE,
    segment_index INT NOT NULL,
    speaker_label VARCHAR(32) NOT NULL,
    start_time_sec NUMERIC(8, 2) NOT NULL,
    end_time_sec NUMERIC(8, 2) NOT NULL,
    text TEXT NOT NULL,
    confidence NUMERIC(4, 3),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_transcripts_record_seq ON consultation_transcripts(record_id, segment_index);

-- 11. Саммари и структурированное медицинское заключение
CREATE TABLE IF NOT EXISTS consultation_summaries (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT REFERENCES consultation_records(id) ON DELETE CASCADE,
    slot_id BIGINT NOT NULL REFERENCES consultation_slots(id) ON DELETE CASCADE,
    doctor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    llm_model_name VARCHAR(64) NOT NULL,
    raw_llm_response JSONB,
    template_code VARCHAR(64) DEFAULT '',
    doctor_description TEXT DEFAULT '',
    complaints TEXT,
    anamnesis TEXT,
    recommendations TEXT NOT NULL,
    diagnostics_plan TEXT,
    follow_up TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    doctor_feedback TEXT,
    approved_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE consultation_summaries ALTER COLUMN record_id DROP NOT NULL;
ALTER TABLE consultation_summaries ADD COLUMN IF NOT EXISTS template_code VARCHAR(64) DEFAULT '';
ALTER TABLE consultation_summaries ADD COLUMN IF NOT EXISTS doctor_description TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_consultation_summaries_client ON consultation_summaries(client_id);

-- 11a. Клинические шаблоны итоговых заключений и рекомендаций (HITL)
CREATE TABLE IF NOT EXISTS consultation_summary_templates (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) UNIQUE NOT NULL,
    title VARCHAR(255) NOT NULL,
    category VARCHAR(128) NOT NULL,
    keywords TEXT NOT NULL DEFAULT '',
    complaints_tpl TEXT NOT NULL DEFAULT '',
    anamnesis_tpl TEXT NOT NULL DEFAULT '',
    recommendations_tpl TEXT NOT NULL DEFAULT '',
    diagnostics_tpl TEXT NOT NULL DEFAULT '',
    follow_up_tpl TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 100
);

-- 12. Официальные медицинские документы и заключения в профиле пациентки
CREATE TABLE IF NOT EXISTS patient_documents (
    id BIGSERIAL PRIMARY KEY,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slot_id BIGINT REFERENCES consultation_slots(id) ON DELETE SET NULL,
    summary_id BIGINT REFERENCES consultation_summaries(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL,
    document_type VARCHAR(64) NOT NULL,
    s3_pdf_key VARCHAR(512) NOT NULL,
    file_size_bytes BIGINT NOT NULL,
    is_published BOOLEAN DEFAULT FALSE,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_patient_documents_client ON patient_documents(client_id, is_published);

-- 13. Одноразовые токены авторизации (Magic Link / OTP)
CREATE TABLE IF NOT EXISTS auth_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(128) UNIQUE NOT NULL,
    token_type VARCHAR(32) NOT NULL DEFAULT 'magic_link',
    target_email VARCHAR(255),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
ALTER TABLE auth_tokens ADD COLUMN IF NOT EXISTS token_type VARCHAR(32) NOT NULL DEFAULT 'magic_link';
ALTER TABLE auth_tokens ADD COLUMN IF NOT EXISTS target_email VARCHAR(255);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_hash ON auth_tokens(token_hash);

-- 14. Состояния FSM-диалога бота MAX
CREATE TABLE IF NOT EXISTS bot_fsm_sessions (
    user_id VARCHAR(128) PRIMARY KEY,
    current_state VARCHAR(64) NOT NULL,
    draft_order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
    context_data JSONB DEFAULT '{}'::jsonb,
    last_callback_id VARCHAR(128),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 15. Дедупликация входящих вебхуков
CREATE TABLE IF NOT EXISTS webhook_events (
    id BIGSERIAL PRIMARY KEY,
    provider VARCHAR(32) NOT NULL,
    event_key VARCHAR(255) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(provider, event_key)
);

-- 16. Вопросы из чат-бота («Задать вопрос»)
CREATE TABLE IF NOT EXISTS questions (
    id BIGSERIAL PRIMARY KEY,
    max_user_id VARCHAR(128),
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    author_name VARCHAR(255),
    question_text TEXT NOT NULL,
    answer_text TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'new',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    answered_at TIMESTAMP WITH TIME ZONE
);

-- 17. Настройки платформы (варианты консультаций, ссылки MAX)
CREATE TABLE IF NOT EXISTS platform_settings (
    key VARCHAR(64) PRIMARY KEY,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 18. Журнал аудита действий
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_role VARCHAR(32),
    action VARCHAR(128) NOT NULL,
    entity_type VARCHAR(64),
    entity_id VARCHAR(64),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);

-- 19. Внутренний чат пациентка <-> врач в веб-кабинете и CRM
CREATE TABLE IF NOT EXISTS chat_messages (
    id BIGSERIAL PRIMARY KEY,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sender_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    sender_role VARCHAR(16) NOT NULL CHECK (sender_role IN ('client', 'doctor', 'admin')),
    body TEXT NOT NULL,
    read_by_client BOOLEAN NOT NULL DEFAULT FALSE,
    read_by_doctor BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_client_created ON chat_messages(client_id, created_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_chat_messages_unread_doc ON chat_messages(client_id) WHERE sender_role = 'client' AND read_by_doctor = FALSE;

-- 20. История ИИ-саммари профиля пациентки (полные, инкрементальные и предконсультационные за 30 мин)
ALTER TABLE consultation_slots ADD COLUMN IF NOT EXISTS pre_summary_generated_at TIMESTAMP WITH TIME ZONE;

CREATE TABLE IF NOT EXISTS patient_ai_summaries (
    id BIGSERIAL PRIMARY KEY,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slot_id BIGINT REFERENCES consultation_slots(id) ON DELETE SET NULL,
    audience VARCHAR(16) NOT NULL DEFAULT 'doctor',
    generation_mode VARCHAR(32) NOT NULL DEFAULT 'full',
    template_code VARCHAR(64) NOT NULL DEFAULT 'avocado_clinical_v1',
    scope_label VARCHAR(255) NOT NULL DEFAULT '',
    summary_text TEXT NOT NULL,
    processed_event_keys JSONB NOT NULL DEFAULT '[]'::jsonb,
    events_count INT NOT NULL DEFAULT 0,
    new_events_count INT NOT NULL DEFAULT 0,
    base_summary_id BIGINT REFERENCES patient_ai_summaries(id) ON DELETE SET NULL,
    llm_model_name VARCHAR(128) NOT NULL DEFAULT 'mock',
    covered_until_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_patient_ai_summaries_client ON patient_ai_summaries(client_id, audience, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_patient_ai_summaries_slot ON patient_ai_summaries(slot_id) WHERE slot_id IS NOT NULL;

-- Предзаполнение тарифов
INSERT INTO tariffs (code, title, description, price_kopecks, duration_days, has_feedback, is_active)
VALUES
    ('self_3m', 'Самостоятельный (3 месяца)', 'Доступ ко всем материалам интенсива на 3 месяца: уроки, чек-листы, памятки, записи лекций', 490000, 90, FALSE, TRUE),
    ('self_6m', 'Самостоятельный (6 месяцев)', 'Доступ ко всем материалам интенсива на 6 месяцев: уроки, чек-листы, памятки, записи лекций', 790000, 180, FALSE, TRUE),
    ('self_9m', 'Самостоятельный (9 месяцев)', 'Доступ ко всем материалам интенсива на весь период беременности (9 месяцев)', 990000, 270, FALSE, TRUE),
    ('self_12m', 'Самостоятельный (12 месяцев)', 'Доступ ко всем материалам интенсива на 1 год, включая блок восстановления после родов', 1290000, 365, FALSE, TRUE),
    ('feedback_3m', 'С обратной связью (3 месяца)', 'Тариф «Самостоятельный» на 3 месяца + персональные консультации врача и ответы на вопросы', 990000, 90, TRUE, TRUE),
    ('feedback_6m', 'С обратной связью (6 месяцев)', 'Тариф «Самостоятельный» на 6 месяцев + персональные консультации врача и ответы на вопросы', 1490000, 180, TRUE, TRUE),
    ('feedback_9m', 'С обратной связью (9 месяцев)', 'Тариф «Самостоятельный» на 9 месяцев + персональные консультации врача и ответы на вопросы', 1890000, 270, TRUE, TRUE),
    ('feedback_12m', 'С обратной связью (12 месяцев)', 'Полное годовое сопровождение беременности и восстановления после родов с обратной связью врача', 2390000, 365, TRUE, TRUE)
ON CONFLICT (code) DO NOTHING;

-- Базовые настройки платформы
INSERT INTO platform_settings (key, value)
VALUES (
    'consultation_config',
    '{"mode":"builtin_slots","doctor_name":"Врач-терапевт, нефролог, нутрициолог","direct_chat_url":"https://max.ru/zdorovie_doctor","external_widget_url":"","max_channel_invite_url":"https://max.ru/zdorovie_private_channel","support_contact":"support@zdorovie.local"}'::jsonb
)
ON CONFLICT (key) DO NOTHING;

-- Базовая программа интенсива (Модули 1-4)
INSERT INTO materials (module_number, lesson_number, title, description, content_markdown, video_url, duration_minutes, file_type, s3_master_key, is_published)
VALUES
    (1, 1, 'Первый триместр: адаптация, токсикоз и базовые дефициты', 'Как перестроить питание в первые недели, безопасно поддержать организм и отличить норму от тревожных симптомов.', '### Ключевые акценты 1-го триместра\n- Мягкая коррекция тошноты и питьевой режим.\n- Базовый чек-лист дефицитов: ферритин, витамин D, фолаты, ТТГ.\n- Красные флаги: когда нужно обратиться к врачу немедленно.', '', 32, 'pdf', 'master/materials/1/source.pdf', TRUE),
    (2, 1, 'Второй триместр: скрининги, анализы и профилактика отёков', 'Разбор обязательных обследований, контроль функции почек и грамотная нутритивная поддержка во 2-м триместре.', '### Ключевые акценты 2-го триместра\n- Расшифровка общего анализа мочи, биохимии и ферритина.\n- Профилактика гестационного диабета и скрытых отёков.\n- Поддержка сосудов и оптимальная дозировка нутриентов.', '', 38, 'pdf', 'master/materials/2/source.pdf', TRUE),
    (3, 1, 'Третий триместр: почки, давление и подготовка к родам', 'Контроль артериального давления, профилактика гестозов, подготовка ресурсов организма к родам.', '### Ключевые акценты 3-го триместра\n- Мониторинг давления и белка в моче.\n- Безопасная тактика при отёках и изжоге.\n- Подготовка организма и сумки в роддом с позиции врача.', '', 40, 'pdf', 'master/materials/3/source.pdf', TRUE),
    (4, 1, 'Послеродовое восстановление и поддержка лактации', 'Восполнение дефицитов после родов, чек-ап показателей здоровья через 6–8 недель и бережное питание.', '### Послеродовой чек-ап\n- Контроль железа, щитовидной железы и белкового обмена.\n- Поддержка лактации без жёстких ограничительных диет.\n- Восстановление сна и энергии.', '', 35, 'pdf', 'master/materials/4/source.pdf', TRUE)
ON CONFLICT (module_number, lesson_number, file_type, title) DO NOTHING;

-- Шаблоны врачебных заключений (HITL)
INSERT INTO consultation_summary_templates (code, title, category, keywords, complaints_tpl, anamnesis_tpl, recommendations_tpl, diagnostics_tpl, follow_up_tpl, sort_order)
VALUES
    ('first_trimester_adaptation', '1 триместр: адаптация и токсикоз', 'Ведение 1 триместра', 'токсикоз,тошнота,рвота,первый триместр,слабость,сонливость,фолиевая,адаптация', 'Жалобы на утреннюю тошноту, снижение аппетита и быструю утомляемость на раннем сроке беременности.', 'Беременность I триместр. Оценены питьевой режим, переносимость пищи и базовый нутритивный статус.', '1. Дробное питание малыми порциями каждые 2–3 часа; избегать резких запахов и длительных перерывов в еде.\n2. Тёплое щелочное питьё небольшими глотками до 1.5–1.8 л/сут при переносимости.\n3. Продолжить базовую поддержку фолатами и йодом в профилактических дозах по согласованию с акушером-гинекологом.', 'ОАК, ТТГ, свободный Т4, ферритин, глюкоза натощак, общий анализ мочи, при выраженной рвоте — электролиты и кетоновые тела.', 'Контроль массы тела и самочувствия через 2 недели. При многократной рвоте (>5 раз/сут) или признаках обезвоживания — очный осмотр врача.', 10),
    ('anemia_ferritin', 'Коррекция дефицита железа и ферритина', 'Дефициты и анемия', 'ферритин,железо,гемоглобин,анемия,усталость,слабость,головокружение,волосы,тахикардия', 'Жалобы на повышенную утомляемость, одышку при обычной нагрузке, снижение концентрации.', 'Выявлено снижение показателей ферритина / тенденция к латентному дефициту железа во время беременности.', '1. Обогащение рациона гемовым железом (нежирная говядина, индейка) в сочетании с продуктами, богатыми витамином С.\n2. Разделить приём железа и кальция/молочных продуктов/чая минимум на 2 часа.\n3. Подбор формы и дозировки препарата железа совместно с лечащим врачом по результату очной оценки переносимости ЖКТ.', 'Ферритин, сывороточное железо, ОЖСС/НТЖ, ОАК с ретикулоцитами, С-реактивный белок (для исключения ложнозавышенного ферритина), витамин B12 и фолаты.', 'Повторный лабораторный контроль ОАК и ферритина через 4 недели от начала коррекции.', 20),
    ('nutrition_gdm', 'Питание, отёки и контроль углеводного обмена (ГСД)', 'Нутрициология и ГСД', 'гсд,диабет,сахар,глюкоза,отек,отёк,отеки,отёки,вес,прибавка,питание,белок,давление', 'Вопросы по коррекции прибавки массы тела, контролю уровня глюкозы и профилактике скрытых отёков.', 'II–III триместр беременности. Разобран дневник питания, баланс белка и сложных углеводов, питьевой режим.', '1. Питание по правилу гарвардской тарелки: акцент на полноценный белок (1.1–1.2 г/кг расчётной массы), клетчатку (овощи 400–500 г/сут) и сложные углеводы с низким ГИ.\n2. Исключить добавленные сахара, сладкие напитки и фруктовые соки; дробный режим 3 основных приёма и 2 перекуса.\n3. Контроль артериального давления и диуреза, умеренная ходьба 20–30 минут после еды.', 'Ведение дневника гликемии (натощак и через 1 час после еды), ОАМ, суточная протеинурия (при показаниях), биохимия (общий белок, альбумин, креатинин, АЛТ/АСТ).', 'Оценка дневника самоконтроля глюкозы и давления через 7–14 дней.', 30),
    ('thyroid_endocrinology', 'Поддержка щитовидной железы и витамин D', 'Эндокринология', 'ттг,щитовид,гипотиреоз,тироксин,эутирокс,йод,витамин d,d3,кальций', 'Запрос на оценку функции щитовидной железы и подбор дозировок витамина D / йода в период беременности.', 'Проанализированы показатели тиреоидного профиля и обеспеченность витамином D с учётом триместра.', '1. Соблюдать интервал не менее 30–40 минут между приёмом левотироксина (если назначен) и завтраком; не совмещать с железом/кальцием в течение 4 часов.\n2. Поддержание адекватного поступления йода (калия йодид при отсутствии противопоказаний) и витамина D по целевому уровню 25(OH)D.\n3. Не корректировать гормональные дозировки самостоятельно без согласования с эндокринологом.', 'ТТГ, свободный Т4, 25(OH)D, общий и ионизированный кальций, паратгормон (по показаниям).', 'Контроль ТТГ и св. Т4 через 4–6 недель или по графику эндокринолога.', 40),
    ('birth_preparation', '3 триместр: почки, давление и подготовка к родам', 'Подготовка к родам', 'третий триместр,роды,роддом,почки,пиелонефрит,белок,давление,гестоз,преэклампсия,изжога,схватки', 'Вопросы подготовки организма к родам, профилактики инфекций мочевыводящих путей, отёков и изжоги на поздних сроках.', 'III триместр беременности. Оценены факторы почечного и сосудистого риска, самочувствие и план родоразрешения.', '1. Ежедневный мониторинг артериального давления утром и вечером с фиксацией показателей в дневник.\n2. Позиционная разгрузка почек (коленно-локтевое положение 10–15 мин 2–3 раза в день), сон преимущественно на левом боку.\n3. При изжоге — ранний лёгкий ужин за 2.5–3 часа до сна, высокое изголовье, согласованные с врачом антациды.', 'ОАМ 1 раз в 2 недели, посев мочи (по показаниям), УЗИ почек при болях в пояснице, КТГ и допплерометрия по сроку.', 'Немедленное обращение за очной помощью при повышении АД ≥140/90 мм рт. ст., резких отёках, головной боли или изменении шевелений плода.', 50),
    ('postpartum_recovery', 'Послеродовое восстановление и грудное вскармливание', 'После родов и ГВ', 'после родов,восстановление,гв,лактация,кормление,молоко,тазовое,дефицит,чек-ап,чекап', 'Восстановление энергии после родов, вопросы рациона кормящей мамы и восполнения послеродовых дефицитов.', 'Послеродовой период / грудное вскармливание. Обсуждены режим сна, восстановление ресурсов и профилактика перегрузок.', '1. Разнообразное полноценное питание без неоправданных жёстких диет; достаточное питьё по жажде (ориентир ~2 л/сут).\n2. Постепенное возвращение физической активности и мягкая работа с мышцами тазового дна после разрешения гинеколога (обычно с 6 недели).\n3. Продолжение нутритивной поддержки с учётом потребностей лактации и результатов послеродового чек-апа.', 'Послеродовой чек-ап через 6–8 недель: ОАК, ферритин, общий белок, ТТГ, 25(OH)D, витамин B12.', 'Повторная консультация по результатам анализов через 6–8 недель после родов.', 60)
ON CONFLICT (code) DO NOTHING;
