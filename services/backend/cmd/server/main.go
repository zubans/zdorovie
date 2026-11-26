package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/httpapi"
	"zdorovie/backend/internal/mailer"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/pdf"
	"zdorovie/backend/internal/queue"
	"zdorovie/backend/internal/s3"
	"zdorovie/backend/internal/scheduler"
	"zdorovie/backend/internal/yukassa"
)

func main() {
	cfg := config.Load()

	if cfg.MaxWebhookSecret == "" && cfg.AppEnv != "test" {
		log.Fatal("CRITICAL: MAX_WEBHOOK_SECRET environment variable must be set")
	}
	if cfg.JWTSecret == "" || cfg.JWTSecret == "change_me_to_random_jwt_secret" {
		log.Println("WARNING: using default or empty JWT_SECRET; set a strong random value in production")
	}

	rootCtx, cancelRoot := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelRoot()

	store, err := db.New(rootCtx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect postgresql: %v", err)
	}
	defer store.Close()

	// Retry initial schema migration + bootstrap gently in case PostgreSQL is still starting
	for attempt := 1; attempt <= 10; attempt++ {
		if err := store.Ping(rootCtx); err == nil {
			break
		}
		log.Printf("waiting for postgresql (attempt %d/10)...", attempt)
		time.Sleep(2 * time.Second)
	}
	if err := store.Migrate(rootCtx); err != nil {
		log.Fatalf("database schema migration failed: %v", err)
	}

	pwHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatalf("failed to hash admin password: %v", err)
	}
	apiTokHash := ""
	if cfg.DoctorAPIToken != "" {
		apiTokHash = auth.HashToken(cfg.DoctorAPIToken)
	}
	adminID, err := store.BootstrapAdmin(rootCtx, cfg.AdminEmail, pwHash, apiTokHash)
	if err != nil {
		log.Fatalf("bootstrap admin failed: %v", err)
	}

	s3Client, err := s3.New(cfg)
	if err != nil {
		log.Fatalf("failed to initialize S3 client: %v", err)
	}
	if err := s3Client.EnsureBuckets(rootCtx); err != nil {
		log.Printf("WARNING: S3 bucket check returned error: %v", err)
	} else {
		bootstrapStarterContent(rootCtx, store, s3Client, adminID)
	}

	qProducer := queue.NewProducer(cfg)
	defer qProducer.Close()

	mailSvc := mailer.New(cfg)
	maxClient := maxbot.NewClient(cfg)
	yooSvc := yukassa.NewService(cfg, store, qProducer, mailSvc, maxClient)
	fsmEngine := maxbot.NewEngine(store, maxClient, yooSvc).WithServices(s3Client, mailSvc, cfg.PublicBaseURL)

	// Initialize live MAX Bot API (webhook subscription on HTTPS, or GET /updates long-polling locally)
	if maxClient.IsLive() {
		go func() {
			meCtx, cancel := context.WithTimeout(rootCtx, 10*time.Second)
			profile, err := maxClient.GetMe(meCtx)
			cancel()
			if err != nil {
				log.Printf("WARNING: failed to verify MAX bot (/me): %v", err)
			} else {
				log.Printf("MAX bot connected: %s (@%s, id=%d)", profile.FirstName, profile.Username, profile.UserID)
			}

			if strings.HasPrefix(cfg.PublicBaseURL, "https://") {
				subCtx, subCancel := context.WithTimeout(rootCtx, 10*time.Second)
				defer subCancel()
				if err := maxClient.RegisterSubscription(subCtx, strings.TrimRight(cfg.PublicBaseURL, "/")+"/api/v1/webhooks/max"); err != nil {
					log.Printf("WARNING: failed to register MAX webhook subscription: %v", err)
				}
				return
			}

			log.Printf("MAX local mode (public_base_url=%s): starting long-polling GET /updates", cfg.PublicBaseURL)
			maxClient.StartPolling(rootCtx, fsmEngine)
		}()
	}

	sched := scheduler.New(cfg, store, yooSvc, maxClient)
	sched.Start(rootCtx, 1*time.Minute)

	apiServer := httpapi.NewServer(cfg, store, s3Client, qProducer, mailSvc, maxClient, fsmEngine, yooSvc, nil)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("starting zdorovie backend on port %s (env=%s max_mode=%s yoo_mode=%s)", cfg.Port, cfg.AppEnv, cfg.MaxMode, cfg.YooMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-rootCtx.Done()
	log.Println("shutting down zdorovie backend gracefully...")

	shutCtx, cancelShut := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShut()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	os.Stdout.Sync()
}

func bootstrapStarterContent(ctx context.Context, store *db.Store, s3Client *s3.Client, doctorID int64) {
	mats, err := store.ListMaterials(ctx, false)
	if err == nil {
		for _, m := range mats {
			if m.FileType != "pdf" || m.S3MasterKey == "" {
				continue
			}
			// Upload starter master PDF only if missing in S3 so admin uploads are preserved.
			if _, err := s3Client.GetBytes(ctx, s3Client.BucketMaster, m.S3MasterKey); err != nil {
				pdfBytes, genErr := pdf.GenerateStarterMasterPDF(m.ModuleNumber, m.LessonNumber, m.Title, m.Description, m.ContentMarkdown)
				if genErr == nil {
					if putErr := s3Client.PutBytes(ctx, s3Client.BucketMaster, m.S3MasterKey, pdfBytes, "application/pdf"); putErr != nil {
						log.Printf("bootstrap starter PDF %s error: %v", m.S3MasterKey, putErr)
					} else {
						log.Printf("uploaded starter master PDF to S3: %s (%d bytes)", m.S3MasterKey, len(pdfBytes))
					}
				}
			}
		}
	}

	// Seed 4 starter available consultation slots if table is currently empty.
	existingSlots, err := store.ListConsultationSlots(ctx, false, 0, "")
	if err == nil && len(existingSlots) == 0 && doctorID > 0 {
		now := time.Now().UTC().Truncate(time.Hour)
		for i := 1; i <= 4; i++ {
			start := now.Add(time.Duration(24*i+7) * time.Hour) // 10:00 MSK (07:00 UTC)
			end := start.Add(45 * time.Minute)
			_, _ = store.CreateConsultationSlot(ctx, doctorID, start, end, "https://telemost.yandex.ru/j/zdorovie-demo-room")
		}
		log.Println("seeded 4 initial consultation slots")
	}
}
