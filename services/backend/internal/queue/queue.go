package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"zdorovie/backend/internal/config"
)

const (
	TaskPDFWatermark  = "pdf:watermark"
	TaskEmailSend     = "email:send"
	TaskMaxNotify     = "max:notify"
	QueueWatermark    = "watermark"
	QueueNotify       = "notifications"
	QueueDefault      = "default"
	RedisMLTranscribe = "zdorovie:queue:audio_transcribe"
	RedisMLSummarize  = "zdorovie:queue:text_summarize"
	RedisMLFailed     = "zdorovie:queue:ml_failed"
	RedisMLStats      = "zdorovie:stats:ml"
)

// WatermarkPayload describes a personalized PDF generation job.
type WatermarkPayload struct {
	ClientMaterialID int64  `json:"client_material_id"`
	UserID           int64  `json:"user_id"`
	OrderID          int64  `json:"order_id"`
	OrderNumber      string `json:"order_number"`
	MaterialID       int    `json:"material_id"`
	FullName         string `json:"full_name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	MasterKey        string `json:"master_key"`
	ClientKey        string `json:"client_key"`
}

// EmailPayload describes an outbound notification email task.
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// MaxNotifyPayload describes an outbound MAX bot message task.
type MaxNotifyPayload struct {
	MaxUserID string `json:"max_user_id"`
	Text      string `json:"text"`
	ButtonURL string `json:"button_url,omitempty"`
	ButtonTxt string `json:"button_text,omitempty"`
}

// AudioTranscribePayload is consumed by Python ml-worker from Redis.
type AudioTranscribePayload struct {
	RecordID   int64  `json:"record_id"`
	SlotID     int64  `json:"slot_id"`
	DoctorID   int64  `json:"doctor_id"`
	ClientID   int64  `json:"client_id"`
	S3AudioKey string `json:"s3_audio_key"`
	Format     string `json:"audio_format"`
}

// QueueStats summarizes Asynq and ML worker queue counters for the admin UI.
type QueueStats struct {
	AsynqQueues []QueueSummary `json:"asynq_queues"`
	ML          MLQueueStats   `json:"ml"`
}

// QueueSummary represents metrics for one Asynq queue.
type QueueSummary struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Processed int    `json:"processed"`
	Failed    int    `json:"failed"`
}

// MLQueueStats tracks pending/processed/failed jobs in ml-worker.
type MLQueueStats struct {
	TranscribePending int64 `json:"transcribe_pending"`
	SummarizePending  int64 `json:"summarize_pending"`
	FailedCount       int64 `json:"failed_count"`
	ProcessedCount    int64 `json:"processed_count"`
}

// Producer enqueues jobs for Go worker (Asynq) and Python ml-worker (Redis).
type Producer struct {
	asynqClient *asynq.Client
	inspector   *asynq.Inspector
	redis       *redis.Client
}

// NewProducer connects Asynq client/inspector and Redis client.
func NewProducer(cfg config.Config) *Producer {
	opt := asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr(),
		Password: cfg.RedisPass,
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr(),
		Password: cfg.RedisPass,
	})
	return &Producer{
		asynqClient: asynq.NewClient(opt),
		inspector:   asynq.NewInspector(opt),
		redis:       rdb,
	}
}

// Close shuts down Redis and Asynq connections.
func (p *Producer) Close() {
	if p.asynqClient != nil {
		_ = p.asynqClient.Close()
	}
	if p.inspector != nil {
		_ = p.inspector.Close()
	}
	if p.redis != nil {
		_ = p.redis.Close()
	}
}

// Redis returns the underlying go-redis client for deduplication/caching.
func (p *Producer) Redis() *redis.Client {
	return p.redis
}

// EnqueueWatermark queues a PDF watermarking job with up to 5 retries.
func (p *Producer) EnqueueWatermark(payload WatermarkPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskPDFWatermark, raw, asynq.MaxRetry(5), asynq.Queue(QueueWatermark), asynq.Timeout(3*time.Minute))
	_, err = p.asynqClient.Enqueue(task)
	return err
}

// EnqueueEmail queues an async email delivery job.
func (p *Producer) EnqueueEmail(payload EmailPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskEmailSend, raw, asynq.MaxRetry(3), asynq.Queue(QueueNotify))
	_, err = p.asynqClient.Enqueue(task)
	return err
}

// EnqueueAudioTranscribe pushes an audio transcription job to Redis for Python ml-worker.
func (p *Producer) EnqueueAudioTranscribe(ctx context.Context, payload AudioTranscribePayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return p.redis.LPush(ctx, RedisMLTranscribe, string(raw)).Err()
}

// Stats returns combined Asynq + ML queue statistics.
func (p *Producer) Stats(ctx context.Context) (QueueStats, error) {
	var out QueueStats
	if p.inspector != nil {
		queues, err := p.inspector.Queues()
		if err == nil {
			for _, qName := range queues {
				info, iErr := p.inspector.GetQueueInfo(qName)
				if iErr != nil {
					continue
				}
				out.AsynqQueues = append(out.AsynqQueues, QueueSummary{
					Name:      info.Queue,
					Pending:   info.Pending,
					Active:    info.Active,
					Retry:     info.Retry,
					Archived:  info.Archived,
					Processed: info.Processed,
					Failed:    info.Failed,
				})
			}
		}
	}
	if p.redis != nil {
		out.ML.TranscribePending, _ = p.redis.LLen(ctx, RedisMLTranscribe).Result()
		out.ML.SummarizePending, _ = p.redis.LLen(ctx, RedisMLSummarize).Result()
		out.ML.FailedCount, _ = p.redis.LLen(ctx, RedisMLFailed).Result()
		if val, err := p.redis.HGet(ctx, RedisMLStats, "processed").Int64(); err == nil {
			out.ML.ProcessedCount = val
		}
	}
	return out, nil
}

// Ping checks Redis connectivity.
func (p *Producer) Ping(ctx context.Context) error {
	if p.redis == nil {
		return fmt.Errorf("redis client not configured")
	}
	return p.redis.Ping(ctx).Err()
}
