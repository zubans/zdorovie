package db

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the PostgreSQL connection pool and domain queries.
type Store struct {
	Pool *pgxpool.Pool
}

// New connects a pgxpool using the provided DSN.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pg config: %w", err)
	}
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pg pool: %w", err)
	}
	return &Store{Pool: pool}, nil
}

// Close releases all pool connections.
func (s *Store) Close() {
	if s.Pool != nil {
		s.Pool.Close()
	}
}

// Ping verifies database connectivity with SELECT 1.
func (s *Store) Ping(ctx context.Context) error {
	if s.Pool == nil {
		return fmt.Errorf("database pool not initialized")
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var one int
	if err := s.Pool.QueryRow(c, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return fmt.Errorf("unexpected SELECT 1 result: %d", one)
	}
	return nil
}

// Migrate applies the idempotent schema DDL.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, schemaSQL)
	return err
}

// Domain Models

type Tariff struct {
	ID           int       `json:"id"`
	Code         string    `json:"code"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	PriceKopecks int64     `json:"price_kopecks"`
	DurationDays int       `json:"duration_days"`
	HasFeedback  bool      `json:"has_feedback"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
}

type User struct {
	ID              int64     `json:"id"`
	MaxUserID       *string   `json:"max_user_id,omitempty"`
	MaxUsername     *string   `json:"max_username,omitempty"`
	FullName        string    `json:"full_name"`
	Phone           string    `json:"phone"`
	Email           string    `json:"email"`
	PregnancyStatus string    `json:"pregnancy_status"`
	Role            string    `json:"role"`
	PasswordHash    string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Order struct {
	ID                  int64      `json:"id"`
	OrderNumber         string     `json:"order_number"`
	UserID              int64      `json:"user_id"`
	TariffID            int        `json:"tariff_id"`
	AmountKopecks       int64      `json:"amount_kopecks"`
	Status              string     `json:"status"`
	ConsentAcceptedAt   *time.Time `json:"consent_accepted_at,omitempty"`
	PaidAt              *time.Time `json:"paid_at,omitempty"`
	ChannelAccessStatus string     `json:"channel_access_status"`
	ChannelApprovedAt   *time.Time `json:"channel_approved_at,omitempty"`
	ChannelJoinedAt     *time.Time `json:"channel_joined_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`

	// Joined fields for admin & cabinet views
	UserFullName   string  `json:"user_full_name,omitempty"`
	UserEmail      string  `json:"user_email,omitempty"`
	UserPhone      string  `json:"user_phone,omitempty"`
	UserMaxID      *string `json:"user_max_id,omitempty"`
	TariffTitle    string  `json:"tariff_title,omitempty"`
	TariffCode     string  `json:"tariff_code,omitempty"`
	YooPaymentID   string  `json:"yukassa_payment_id,omitempty"`
	PaymentStatus  string  `json:"payment_status,omitempty"`
	ReceiptURL     string  `json:"receipt_url,omitempty"`
	ConfirmURL     string  `json:"confirmation_url,omitempty"`
}

type Payment struct {
	ID               int64           `json:"id"`
	OrderID          int64           `json:"order_id"`
	YooPaymentID     string          `json:"yukassa_payment_id"`
	Status           string          `json:"status"`
	AmountKopecks    int64           `json:"amount_kopecks"`
	IsNPDRegistered  bool            `json:"is_npd_registered"`
	ReceiptURL       string          `json:"receipt_url"`
	ConfirmationURL  string          `json:"confirmation_url"`
	ExpiresAt        *time.Time      `json:"expires_at,omitempty"`
	RawPayload       json.RawMessage `json:"raw_payload,omitempty"`
	PaidAt           *time.Time      `json:"paid_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type Subscription struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	OrderID    int64     `json:"order_id"`
	TariffID   int       `json:"tariff_id"`
	StartsAt   time.Time `json:"starts_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	TariffCode string    `json:"tariff_code,omitempty"`
	TariffName string    `json:"tariff_title,omitempty"`
	HasFeedback bool     `json:"has_feedback"`
	DaysLeft   int       `json:"days_left"`
}

type Material struct {
	ID              int       `json:"id"`
	ModuleNumber    int       `json:"module_number"`
	LessonNumber    int       `json:"lesson_number"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	ContentMarkdown string    `json:"content_markdown"`
	VideoURL        string    `json:"video_url"`
	DurationMinutes int       `json:"duration_minutes"`
	FileType        string    `json:"file_type"`
	S3MasterKey     string    `json:"s3_master_key"`
	IsPublished     bool      `json:"is_published"`
	CreatedAt       time.Time `json:"created_at"`
}

type CabinetMaterial struct {
	Material
	ClientStatus string     `json:"client_status"` // pending, processing, ready, failed, none
	Completed    bool       `json:"completed"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	GeneratedAt  *time.Time `json:"generated_at,omitempty"`
}

type ClientMaterial struct {
	ID                 int64      `json:"id"`
	UserID             int64      `json:"user_id"`
	OrderID            int64      `json:"order_id"`
	MaterialID         int        `json:"material_id"`
	S3ClientKey        string     `json:"s3_client_key"`
	WatermarkSignature string     `json:"watermark_signature"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"error_message"`
	GeneratedAt        *time.Time `json:"generated_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type ConsultationConfig struct {
	Mode                 string `json:"mode"` // direct_chat, external_widget, builtin_slots
	DoctorName           string `json:"doctor_name"`
	DirectChatURL        string `json:"direct_chat_url"`
	ExternalWidgetURL    string `json:"external_widget_url"`
	MaxChannelInviteURL  string `json:"max_channel_invite_url"`
	SupportContact       string `json:"support_contact"`
}

type ConsultationSlot struct {
	ID             int64      `json:"id"`
	DoctorID       int64      `json:"doctor_id"`
	ClientID       *int64     `json:"client_id,omitempty"`
	OrderID        *int64     `json:"order_id,omitempty"`
	SlotStart      time.Time  `json:"slot_start"`
	SlotEnd        time.Time  `json:"slot_end"`
	Status         string     `json:"status"`
	MeetingURL     string     `json:"meeting_url"`
	ClientQuestion string     `json:"client_question"`
	DoctorNotes    string     `json:"doctor_notes,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DoctorName     string     `json:"doctor_name,omitempty"`
	ClientName     string     `json:"client_name,omitempty"`
	ClientEmail    string     `json:"client_email,omitempty"`
	ClientPreg     string     `json:"client_pregnancy_status,omitempty"`
	RecordID       *int64     `json:"record_id,omitempty"`
	RecordStatus   string     `json:"record_status,omitempty"`
	SummaryID      *int64     `json:"summary_id,omitempty"`
	SummaryStatus  string     `json:"summary_status,omitempty"`
}

type ConsultationAttachment struct {
	ID          int64     `json:"id"`
	SlotID      int64     `json:"slot_id"`
	ClientID    int64     `json:"client_id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	FileSize    int64     `json:"file_size_bytes"`
	S3Key       string    `json:"s3_key"`
	CreatedAt   time.Time `json:"created_at"`
}

type ConsultationRecord struct {
	ID              int64     `json:"id"`
	SlotID          int64     `json:"slot_id"`
	DoctorID        int64     `json:"doctor_id"`
	ClientID        int64     `json:"client_id"`
	SourceType      string    `json:"source_type"`
	OriginalFile    string    `json:"original_file_name"`
	S3AudioKey      string    `json:"s3_audio_key"`
	FileSizeBytes   int64     `json:"file_size_bytes"`
	DurationSeconds *int      `json:"duration_seconds,omitempty"`
	AudioFormat     string    `json:"audio_format"`
	Status          string    `json:"status"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	RecordedAt      time.Time `json:"recorded_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type TranscriptSegment struct {
	ID           int64    `json:"id"`
	RecordID     int64    `json:"record_id"`
	SegmentIndex int      `json:"segment_index"`
	SpeakerLabel string   `json:"speaker_label"`
	StartTimeSec float64  `json:"start_time_sec"`
	EndTimeSec   float64  `json:"end_time_sec"`
	Text         string   `json:"text"`
	Confidence   *float64 `json:"confidence,omitempty"`
}

type ConsultationSummary struct {
	ID                int64      `json:"id"`
	RecordID          *int64     `json:"record_id,omitempty"`
	SlotID            int64      `json:"slot_id"`
	DoctorID          int64      `json:"doctor_id"`
	ClientID          int64      `json:"client_id"`
	LLMModelName      string     `json:"llm_model_name"`
	TemplateCode      string     `json:"template_code"`
	DoctorDescription string     `json:"doctor_description"`
	Complaints        string     `json:"complaints"`
	Anamnesis         string     `json:"anamnesis"`
	Recommendations   string     `json:"recommendations"`
	DiagnosticsPlan   string     `json:"diagnostics_plan"`
	FollowUp          string     `json:"follow_up"`
	Status            string     `json:"status"`
	DoctorFeedback    string     `json:"doctor_feedback"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type SummaryTemplate struct {
	ID                 int    `json:"id"`
	Code               string `json:"code"`
	Title              string `json:"title"`
	Category           string `json:"category"`
	Keywords           string `json:"keywords"`
	ComplaintsTpl      string `json:"complaints_tpl"`
	AnamnesisTpl       string `json:"anamnesis_tpl"`
	RecommendationsTpl string `json:"recommendations_tpl"`
	DiagnosticsTpl     string `json:"diagnostics_tpl"`
	FollowUpTpl        string `json:"follow_up_tpl"`
}

type PatientDocument struct {
	ID            int64      `json:"id"`
	ClientID      int64      `json:"client_id"`
	SlotID        *int64     `json:"slot_id,omitempty"`
	SummaryID     *int64     `json:"summary_id,omitempty"`
	Title         string     `json:"title"`
	DocumentType  string     `json:"document_type"`
	S3PDFKey      string     `json:"s3_pdf_key"`
	FileSizeBytes int64      `json:"file_size_bytes"`
	IsPublished   bool       `json:"is_published"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`

	// Embedded summary details for cabinet view
	DoctorName      string `json:"doctor_name,omitempty"`
	Complaints      string `json:"complaints,omitempty"`
	Anamnesis       string `json:"anamnesis,omitempty"`
	Recommendations string `json:"recommendations,omitempty"`
	DiagnosticsPlan string `json:"diagnostics_plan,omitempty"`
	FollowUp        string `json:"follow_up,omitempty"`
}

type BotFSMSession struct {
	UserID         string                 `json:"user_id"`
	CurrentState   string                 `json:"current_state"`
	DraftOrderID   *int64                 `json:"draft_order_id,omitempty"`
	ContextData    map[string]interface{} `json:"context_data"`
	LastCallbackID string                 `json:"last_callback_id"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type Question struct {
	ID               int64      `json:"id"`
	MaxUserID        *string    `json:"max_user_id,omitempty"`
	UserID           *int64     `json:"user_id,omitempty"`
	AuthorName       string     `json:"author_name"`
	QuestionText     string     `json:"question_text"`
	AnswerText       string     `json:"answer_text"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	AnsweredAt       *time.Time `json:"answered_at,omitempty"`
	MatchedUserID    *int64     `json:"matched_user_id,omitempty"`
	ClientName       string     `json:"client_name,omitempty"`
	ClientPregStatus string     `json:"client_pregnancy_status,omitempty"`
	TariffTitle      string     `json:"tariff_title,omitempty"`
	SubActive        bool       `json:"subscription_active"`
}

type ChatMessage struct {
	ID           int64     `json:"id"`
	ClientID     int64     `json:"client_id"`
	SenderUserID *int64    `json:"sender_user_id,omitempty"`
	SenderRole   string    `json:"sender_role"`
	SenderName   string    `json:"sender_name,omitempty"`
	Body         string    `json:"body"`
	ReadByClient bool      `json:"read_by_client"`
	ReadByDoctor bool      `json:"read_by_doctor"`
	CreatedAt    time.Time `json:"created_at"`
}

type PatientAISummary struct {
	ID                 int64     `json:"id"`
	ClientID           int64     `json:"client_id"`
	SlotID             *int64    `json:"slot_id,omitempty"`
	Audience           string    `json:"audience"`        // doctor | patient
	GenerationMode     string    `json:"generation_mode"` // full | incremental | custom | pre_consultation
	TemplateCode       string    `json:"template_code"`
	ScopeLabel         string    `json:"scope_label"`
	SummaryText        string    `json:"summary_text"`
	ProcessedEventKeys []string  `json:"processed_event_keys"`
	EventsCount        int       `json:"events_count"`
	NewEventsCount     int       `json:"new_events_count"`
	BaseSummaryID      *int64    `json:"base_summary_id,omitempty"`
	LLMModelName       string    `json:"llm_model_name"`
	CoveredUntilAt     time.Time `json:"covered_until_at"`
	CreatedAt          time.Time `json:"created_at"`
}

type ClientProfile struct {
	User            User                     `json:"user"`
	Subscription    *Subscription            `json:"subscription"`
	SubActive       bool                     `json:"sub_active"`
	Materials       []CabinetMaterial        `json:"materials"`
	Consultations   []ConsultationSlot       `json:"consultations"`
	Attachments     []ConsultationAttachment `json:"attachments"`
	Documents       []PatientDocument        `json:"documents"`
	Questions       []Question               `json:"questions"`
	Orders          []Order                  `json:"orders"`
	Messages        []ChatMessage            `json:"messages"`
	UnreadByDoctor  int                      `json:"unread_by_doctor"`
	LatestAISummary *PatientAISummary        `json:"latest_ai_summary,omitempty"`
}

type ClientListRow struct {
	ID                 int64      `json:"id"`
	MaxUserID          *string    `json:"max_user_id,omitempty"`
	FullName           string     `json:"full_name"`
	Phone              string     `json:"phone"`
	Email              string     `json:"email"`
	PregnancyStatus    string     `json:"pregnancy_status"`
	CreatedAt          time.Time  `json:"created_at"`
	TariffTitle        string     `json:"tariff_title"`
	TariffCode         string     `json:"tariff_code"`
	HasFeedback        bool       `json:"has_feedback"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	SubActive          bool       `json:"subscription_active"`
	CompletedSlots     int        `json:"completed_consultations"`
	BookedSlots        int        `json:"booked_consultations"`
	MaterialsReady     int        `json:"materials_ready"`
	MaterialsTotal     int        `json:"materials_total"`
	UnreadChatCount    int        `json:"unread_chat_count"`
	TotalChatCount     int        `json:"total_chat_count"`
	LastChatPreview    string     `json:"last_chat_preview,omitempty"`
	LastChatSenderRole string     `json:"last_chat_sender_role,omitempty"`
	LastChatAt         *time.Time `json:"last_chat_at,omitempty"`
}

type AnalyticsSummary struct {
	TotalRevenueKopecks int64             `json:"total_revenue_kopecks"`
	PaidOrdersCount     int               `json:"paid_orders_count"`
	PendingOrdersCount  int               `json:"pending_orders_count"`
	ActiveClientsCount  int               `json:"active_clients_count"`
	PendingReviewsCount int               `json:"pending_reviews_count"`
	BotSessionsCount    int               `json:"bot_sessions_count"`
	ConversionPercent   float64           `json:"conversion_percent"`
	ByTariff            []TariffStatRow   `json:"by_tariff"`
}

type TariffStatRow struct {
	Code           string `json:"code"`
	Title          string `json:"title"`
	OrdersCount    int    `json:"orders_count"`
	RevenueKopecks int64  `json:"revenue_kopecks"`
}

type AuditLog struct {
	ID          int64           `json:"id"`
	ActorUserID *int64          `json:"actor_user_id,omitempty"`
	ActorRole   string          `json:"actor_role"`
	Action      string          `json:"action"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at"`
}
