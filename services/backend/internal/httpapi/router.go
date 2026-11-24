package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/mailer"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/queue"
	"zdorovie/backend/internal/s3"
	"zdorovie/backend/internal/yukassa"
)

type ctxKey string

const userCtxKey ctxKey = "auth_user"

// HealthResponse preserves the /healthz contract used across monitoring & integration tests.
type HealthResponse struct {
	Status    string `json:"status"`
	DB        string `json:"db"`
	Redis     string `json:"redis"`
	Timestamp string `json:"timestamp"`
}

// HealthChecker defines backend health probes.
type HealthChecker interface {
	CheckDB(ctx context.Context) error
	CheckRedis(ctx context.Context) error
}

// DefaultHealthChecker checks PostgreSQL via pgxpool and Redis via queue/redis ping.
type DefaultHealthChecker struct {
	Store *db.Store
	Queue *queue.Producer
}

func (c *DefaultHealthChecker) CheckDB(ctx context.Context) error {
	if c.Store == nil {
		return errors.New("db store not configured")
	}
	return c.Store.Ping(ctx)
}

func (c *DefaultHealthChecker) CheckRedis(ctx context.Context) error {
	if c.Queue == nil {
		return errors.New("redis queue not configured")
	}
	return c.Queue.Ping(ctx)
}

// Server holds HTTP dependencies for the backend REST & webhook API.
type Server struct {
	cfg     config.Config
	store   *db.Store
	s3      *s3.Client
	queue   *queue.Producer
	mailer  *mailer.Service
	bot     *maxbot.Client
	fsm     *maxbot.Engine
	yoo     *yukassa.Service
	checker HealthChecker
}

// NewServer constructs an HTTP API Server.
func NewServer(
	cfg config.Config,
	store *db.Store,
	s3Client *s3.Client,
	q *queue.Producer,
	mail *mailer.Service,
	bot *maxbot.Client,
	fsm *maxbot.Engine,
	yoo *yukassa.Service,
	checker HealthChecker,
) *Server {
	if checker == nil {
		checker = &DefaultHealthChecker{Store: store, Queue: q}
	}
	return &Server{
		cfg:     cfg,
		store:   store,
		s3:      s3Client,
		queue:   q,
		mailer:  mail,
		bot:     bot,
		fsm:     fsm,
		yoo:     yoo,
		checker: checker,
	}
}

// Handler wires all HTTP endpoints and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health checks
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/healthz", s.handleHealthz)

	// Webhooks (supports both /api/v1/webhooks/* and legacy /webhooks/* aliases)
	mux.HandleFunc("POST /api/v1/webhooks/max", s.handleMaxWebhook)
	mux.HandleFunc("POST /webhooks/max", s.handleMaxWebhook)
	mux.HandleFunc("POST /api/v1/webhooks/yukassa", s.handleYooKassaWebhook)
	mux.HandleFunc("POST /webhooks/yukassa", s.handleYooKassaWebhook)
	mux.HandleFunc("POST /webhooks/yookassa", s.handleYooKassaWebhook)

	// Public storefront / checkout fallback
	mux.HandleFunc("GET /api/v1/tariffs", s.handleListTariffs)
	mux.HandleFunc("GET /api/v1/consultations/settings", s.handlePublicConsultationSettings)
	mux.HandleFunc("POST /api/v1/public/checkout", s.handlePublicCheckout)

	// Local Dev & Mock Simulators (disabled automatically in strict production mode unless debug)
	mux.HandleFunc("GET /api/v1/dev/mock-pay", s.handleMockPayPage)
	mux.HandleFunc("POST /api/v1/dev/mock-pay/confirm", s.handleMockPayConfirm)
	mux.HandleFunc("GET /api/v1/dev/outbox", s.handleDevOutbox)

	// Auth (Magic Link, Email OTP, Admin/Doctor Login, Session Me)
	mux.HandleFunc("POST /api/v1/auth/magic", s.handleMagicAuth)
	mux.HandleFunc("POST /api/v1/auth/otp/request", s.handleOTPRequest)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", s.handleOTPVerify)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleAdminLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))

	// Patient Cabinet
	mux.HandleFunc("GET /api/v1/cabinet/dashboard", s.requireAuth(s.handleCabinetDashboard))
	mux.HandleFunc("POST /api/v1/cabinet/summary", s.requireAuth(s.requireActiveSub(s.handleCabinetSummary)))
	mux.HandleFunc("GET /api/v1/cabinet/chat", s.requireAuth(s.handleCabinetGetChat))
	mux.HandleFunc("GET /api/v1/cabinet/chat/unread", s.requireAuth(s.handleCabinetChatUnread))
	mux.HandleFunc("POST /api/v1/cabinet/chat", s.requireAuth(s.handleCabinetSendChat))
	mux.HandleFunc("POST /api/v1/cabinet/chat/read", s.requireAuth(s.handleCabinetMarkChatRead))
	mux.HandleFunc("GET /api/v1/cabinet/profile", s.requireAuth(s.handleCabinetProfile))
	mux.HandleFunc("POST /api/v1/cabinet/renew", s.requireAuth(s.handleCabinetRenew))

	// Protected by active subscription
	mux.HandleFunc("GET /api/v1/cabinet/materials", s.requireAuth(s.requireActiveSub(s.handleCabinetMaterials)))
	mux.HandleFunc("POST /api/v1/cabinet/materials/{id}/progress", s.requireAuth(s.requireActiveSub(s.handleCabinetMaterialProgress)))
	mux.HandleFunc("GET /api/v1/cabinet/materials/{id}/download", s.requireAuth(s.requireActiveSub(s.handleCabinetMaterialDownload)))

	mux.HandleFunc("GET /api/v1/cabinet/consultations/slots", s.requireAuth(s.handleCabinetConsultationSlots))
	mux.HandleFunc("POST /api/v1/cabinet/consultations/slots/{id}/book", s.requireAuth(s.requireActiveSub(s.handleCabinetBookSlot)))
	mux.HandleFunc("POST /api/v1/cabinet/consultations/slots/{id}/cancel", s.requireAuth(s.handleCabinetCancelSlot))
	mux.HandleFunc("PATCH /api/v1/cabinet/consultations/slots/{id}/question", s.requireAuth(s.handleCabinetUpdateSlotQuestion))
	mux.HandleFunc("POST /api/v1/cabinet/consultations/slots/{id}/attachments", s.requireAuth(s.requireActiveSub(s.handleCabinetUploadAttachment)))
	mux.HandleFunc("GET /api/v1/cabinet/consultations/slots/{id}/attachments", s.requireAuth(s.handleCabinetListAttachments))

	mux.HandleFunc("GET /api/v1/cabinet/documents", s.requireAuth(s.handleCabinetDocuments))
	mux.HandleFunc("GET /api/v1/cabinet/documents/{id}/download", s.requireAuth(s.handleCabinetDocumentDownload))

	// Admin & CRM API (doctor & admin roles)
	mux.HandleFunc("GET /api/v1/admin/analytics", s.requireStaff(s.handleAdminAnalytics))
	mux.HandleFunc("GET /api/v1/admin/queues", s.requireStaff(s.handleAdminQueues))
	mux.HandleFunc("GET /api/v1/admin/clients", s.requireStaff(s.handleAdminClients))
	mux.HandleFunc("GET /api/v1/admin/chat/unread", s.requireStaff(s.handleAdminChatUnreadSummary))
	mux.HandleFunc("GET /api/v1/admin/clients/{id}/profile", s.requireStaff(s.handleAdminClientProfile))
	mux.HandleFunc("POST /api/v1/admin/clients/{id}/summary", s.requireStaff(s.handleAdminClientSummary))
	mux.HandleFunc("GET /api/v1/admin/clients/{id}/chat", s.requireStaff(s.handleAdminGetClientChat))
	mux.HandleFunc("POST /api/v1/admin/clients/{id}/chat", s.requireStaff(s.handleAdminSendClientChat))
	mux.HandleFunc("POST /api/v1/admin/clients/{id}/chat/read", s.requireStaff(s.handleAdminMarkClientChatRead))
	mux.HandleFunc("PATCH /api/v1/admin/clients/{id}/subscription", s.requireStaff(s.handleAdminUpdateClientSub))
	mux.HandleFunc("POST /api/v1/admin/clients/{id}/magic-link", s.requireStaff(s.handleAdminResendMagicLink))
	mux.HandleFunc("GET /api/v1/admin/clients/{id}/materials", s.requireStaff(s.handleAdminClientMaterials))
	mux.HandleFunc("POST /api/v1/admin/clients/{id}/materials/{materialId}/rewatermark", s.requireStaff(s.handleAdminRewatermark))

	mux.HandleFunc("GET /api/v1/admin/orders", s.requireStaff(s.handleAdminOrders))
	mux.HandleFunc("POST /api/v1/admin/orders/{id}/approve", s.requireStaff(s.handleAdminApproveOrder))

	mux.HandleFunc("GET /api/v1/admin/tariffs", s.requireStaff(s.handleAdminListTariffs))
	mux.HandleFunc("PATCH /api/v1/admin/tariffs/{id}", s.requireStaff(s.handleAdminUpdateTariff))

	mux.HandleFunc("GET /api/v1/admin/materials", s.requireStaff(s.handleAdminListMaterials))
	mux.HandleFunc("POST /api/v1/admin/materials", s.requireStaff(s.handleAdminCreateMaterial))
	mux.HandleFunc("PUT /api/v1/admin/materials/{id}", s.requireStaff(s.handleAdminUpdateMaterial))

	mux.HandleFunc("GET /api/v1/admin/questions", s.requireStaff(s.handleAdminQuestions))
	mux.HandleFunc("POST /api/v1/admin/questions/{id}/reply", s.requireStaff(s.handleAdminReplyQuestion))
	mux.HandleFunc("POST /api/v1/admin/questions/{id}/suggest-replies", s.requireStaff(s.handleAdminQuestionSuggestReplies))

	mux.HandleFunc("GET /api/v1/admin/consultations/settings", s.requireStaff(s.handleAdminGetConsultSettings))
	mux.HandleFunc("PUT /api/v1/admin/consultations/settings", s.requireStaff(s.handleAdminSaveConsultSettings))
	mux.HandleFunc("GET /api/v1/admin/consultations/slots", s.requireStaff(s.handleAdminListSlots))
	mux.HandleFunc("POST /api/v1/admin/consultations/slots", s.requireStaff(s.handleAdminCreateSlot))
	mux.HandleFunc("PATCH /api/v1/admin/consultations/slots/{id}", s.requireStaff(s.handleAdminUpdateSlot))
	mux.HandleFunc("DELETE /api/v1/admin/consultations/slots/{id}", s.requireStaff(s.handleAdminDeleteSlot))
	mux.HandleFunc("GET /api/v1/admin/consultations/attachments/{id}/download", s.requireStaff(s.handleAdminDownloadAttachment))

	// Consultations Media, Audio Recorder, Whisper Transcripts & HITL Summary Approval
	mux.HandleFunc("GET /api/v1/consultations/slots", s.requireStaff(s.handleDoctorSlots))
	mux.HandleFunc("GET /api/v1/consultations/slots/{id}/workspace", s.requireStaff(s.handleSlotWorkspace))
	mux.HandleFunc("POST /api/v1/consultations/slots/{id}/audio", s.requireStaff(s.handleUploadAudio))
	mux.HandleFunc("POST /api/v1/consultations/uploads/init", s.requireStaff(s.handleChunkUploadInit))
	mux.HandleFunc("PUT /api/v1/consultations/uploads/{uploadId}/chunk/{index}", s.requireStaff(s.handleChunkUploadPart))
	mux.HandleFunc("POST /api/v1/consultations/uploads/{uploadId}/complete", s.requireStaff(s.handleChunkUploadComplete))
	mux.HandleFunc("GET /api/v1/consultations/records/{id}/audio-url", s.requireStaff(s.handleAudioPresignedURL))
	mux.HandleFunc("GET /api/v1/consultations/records/{id}/transcript", s.requireStaff(s.handleGetTranscript))
	mux.HandleFunc("POST /api/v1/consultations/records/{id}/retranscribe", s.requireStaff(s.handleRetranscribe))
	mux.HandleFunc("POST /api/v1/consultations/slots/{id}/summary/ai-draft", s.requireStaff(s.handleAIDraftSummary))
	mux.HandleFunc("PUT /api/v1/consultations/slots/{id}/summary", s.requireStaff(s.handleUpsertSlotSummary))
	mux.HandleFunc("PUT /api/v1/consultations/summaries/{id}", s.requireStaff(s.handleUpdateSummaryDraft))
	mux.HandleFunc("POST /api/v1/consultations/summaries/{id}/approve", s.requireStaff(s.handleApproveSummary))

	return recoverMiddleware(corsMiddleware(mux))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dbStatus := "connected"
	redisStatus := "connected"
	overall := "ok"
	httpCode := http.StatusOK

	if err := s.checker.CheckDB(ctx); err != nil {
		dbStatus = "error"
		overall = "degraded"
		httpCode = http.StatusServiceUnavailable
	}
	if err := s.checker.CheckRedis(ctx); err != nil {
		redisStatus = "error"
		overall = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	writeJSON(w, httpCode, HealthResponse{
		Status:    overall,
		DB:        dbStatus,
		Redis:     redisStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) authenticateRequest(r *http.Request) (*db.User, error) {
	tok, ok := auth.ExtractBearerOrCookie(r)
	if !ok {
		return nil, errors.New("unauthorized")
	}
	if claims, err := auth.ParseJWT(s.cfg.JWTSecret, tok); err == nil {
		u, err := s.store.GetUserByID(r.Context(), claims.UserID)
		if err != nil {
			return nil, errors.New("user not found")
		}
		return u, nil
	}
	// Fallback: static Doctor API Bearer Token (used by Windows Desktop Recorder Agent)
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		u, err := s.store.GetUserByAPITokenHash(r.Context(), auth.HashToken(tok))
		if err == nil {
			return u, nil
		}
	}
	return nil, errors.New("invalid session")
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "База данных недоступна")
			return
		}
		u, err := s.authenticateRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) requireStaff(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || (u.Role != "admin" && u.Role != "doctor") {
			writeError(w, http.StatusForbidden, "forbidden", "Доступ только для врача или администратора")
			return
		}
		next(w, r)
	})
}

func (s *Server) requireActiveSub(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
			return
		}
		if u.Role == "admin" || u.Role == "doctor" {
			next(w, r)
			return
		}
		sub, err := s.store.GetLatestSubscription(r.Context(), u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "Ошибка проверки подписки")
			return
		}
		if sub == nil || !sub.IsActive || sub.ExpiresAt.Before(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "subscription_expired", "Срок вашей подписки истёк. Продлите доступ для продолжения.")
			return
		}
		next(w, r)
	}
}

func currentUser(r *http.Request) *db.User {
	u, _ := r.Context().Value(userCtxKey).(*db.User)
	return u
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered on %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	writeJSON(w, code, map[string]string{
		"error":   errCode,
		"message": message,
	})
}

func decodeJSON(r *http.Request, dst interface{}, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	return dec.Decode(dst)
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
