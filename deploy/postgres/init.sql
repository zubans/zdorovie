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
    full_name VARCHAR(255) NOT NULL,
    phone VARCHAR(32) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    pregnancy_status VARCHAR(128),
    role VARCHAR(32) DEFAULT 'client',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 3. Заказы
CREATE TABLE IF NOT EXISTS orders (
    id BIGSERIAL PRIMARY KEY,
    order_number VARCHAR(64) UNIQUE NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    tariff_id INT NOT NULL REFERENCES tariffs(id) ON DELETE RESTRICT,
    amount_kopecks BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 4. Платежи ЮKassa
CREATE TABLE IF NOT EXISTS payments (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    yukassa_payment_id VARCHAR(128) UNIQUE NOT NULL,
    status VARCHAR(64) NOT NULL,
    amount_kopecks BIGINT NOT NULL,
    is_npd_registered BOOLEAN DEFAULT TRUE,
    receipt_url TEXT,
    raw_payload JSONB,
    paid_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 5. Подписки и сроки доступа
CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    tariff_id INT NOT NULL REFERENCES tariffs(id) ON DELETE RESTRICT,
    starts_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_active ON subscriptions(user_id, is_active, expires_at);

-- 6. Эталонные материалы (Master)
CREATE TABLE IF NOT EXISTS materials (
    id SERIAL PRIMARY KEY,
    module_number INT NOT NULL,
    lesson_number INT NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    file_type VARCHAR(32) NOT NULL,
    s3_master_key VARCHAR(512) NOT NULL,
    is_published BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
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
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
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
    record_id BIGINT NOT NULL REFERENCES consultation_records(id) ON DELETE CASCADE,
    slot_id BIGINT NOT NULL REFERENCES consultation_slots(id) ON DELETE CASCADE,
    doctor_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    llm_model_name VARCHAR(64) NOT NULL,
    raw_llm_response JSONB,
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
CREATE INDEX IF NOT EXISTS idx_consultation_summaries_client ON consultation_summaries(client_id);

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

-- 13. Одноразовые токены авторизации (Magic Link)
CREATE TABLE IF NOT EXISTS auth_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(128) UNIQUE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
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
