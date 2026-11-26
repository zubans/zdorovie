package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrSlotOverlap indicates a schedule conflict with an existing doctor consultation slot.
	ErrSlotOverlap = errors.New("slot overlaps with an existing consultation slot")
	// ErrInvalidSlotDuration indicates slot_end <= slot_start or outside allowed bounds.
	ErrInvalidSlotDuration = errors.New("invalid slot time interval")
)

// SlotOverlapError describes the existing slot conflicting with a requested interval.
type SlotOverlapError struct {
	ConflictID    int64
	ConflictStart time.Time
	ConflictEnd   time.Time
}

func (e *SlotOverlapError) Error() string {
	return fmt.Sprintf("пересечение с существующим слотом #%d (%s – %s)",
		e.ConflictID,
		e.ConflictStart.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04"),
		e.ConflictEnd.In(time.FixedZone("MSK", 3*3600)).Format("15:04"),
	)
}

func (e *SlotOverlapError) Unwrap() error {
	return ErrSlotOverlap
}

func validateSlotWindow(start, end time.Time) error {
	if !end.After(start) {
		return fmt.Errorf("%w: время окончания должно быть позже времени начала", ErrInvalidSlotDuration)
	}
	dur := end.Sub(start)
	if dur < 15*time.Minute || dur > 240*time.Minute {
		return fmt.Errorf("%w: длительность слота должна быть от 15 до 240 минут", ErrInvalidSlotDuration)
	}
	return nil
}

// CreateConsultationSlot creates a schedule slot for a doctor with mandatory overlap check.
func (s *Store) CreateConsultationSlot(ctx context.Context, doctorID int64, start, end time.Time, meetingURL string) (*ConsultationSlot, error) {
	if err := validateSlotWindow(start, end); err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Serialize concurrent slot creations for the same doctor to guarantee zero overlaps.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, doctorID); err != nil {
		return nil, err
	}

	var conflictID int64
	var cStart, cEnd time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, slot_start, slot_end
		FROM consultation_slots
		WHERE doctor_id = $1
		  AND status <> 'canceled'
		  AND slot_start < $3
		  AND slot_end > $2
		ORDER BY slot_start ASC
		LIMIT 1`,
		doctorID, start, end,
	).Scan(&conflictID, &cStart, &cEnd)
	if err == nil {
		return nil, &SlotOverlapError{ConflictID: conflictID, ConflictStart: cStart, ConflictEnd: cEnd}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var sl ConsultationSlot
	err = tx.QueryRow(ctx, `
		INSERT INTO consultation_slots (doctor_id, slot_start, slot_end, status, meeting_url)
		VALUES ($1, $2, $3, 'available', $4)
		RETURNING id, doctor_id, client_id, order_id, slot_start, slot_end, status, COALESCE(meeting_url,''), COALESCE(client_question,''), COALESCE(doctor_notes,''), created_at, updated_at`,
		doctorID, start, end, meetingURL,
	).Scan(&sl.ID, &sl.DoctorID, &sl.ClientID, &sl.OrderID, &sl.SlotStart, &sl.SlotEnd, &sl.Status, &sl.MeetingURL, &sl.ClientQuestion, &sl.DoctorNotes, &sl.CreatedAt, &sl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &sl, nil
}

// ListConsultationSlots returns slots filtered by status/client/doctor.
func (s *Store) ListConsultationSlots(ctx context.Context, includeDoctorNotes bool, clientID int64, statusFilter string) ([]ConsultationSlot, error) {
	q := `
		SELECT cs.id, cs.doctor_id, cs.client_id, cs.order_id, cs.slot_start, cs.slot_end, cs.status,
		       COALESCE(cs.meeting_url,''), COALESCE(cs.client_question,''),
		       CASE WHEN $1 THEN COALESCE(cs.doctor_notes,'') ELSE '' END,
		       cs.created_at, cs.updated_at,
		       doc.full_name,
		       COALESCE(cli.full_name,''), COALESCE(cli.email,''), COALESCE(cli.pregnancy_status,''),
		       cr.id, COALESCE(cr.status,''),
		       sum.id, COALESCE(sum.status,'')
		FROM consultation_slots cs
		JOIN users doc ON doc.id = cs.doctor_id
		LEFT JOIN users cli ON cli.id = cs.client_id
		LEFT JOIN LATERAL (
			SELECT id, status FROM consultation_records WHERE slot_id = cs.id ORDER BY id DESC LIMIT 1
		) cr ON TRUE
		LEFT JOIN LATERAL (
			SELECT id, status FROM consultation_summaries WHERE slot_id = cs.id ORDER BY id DESC LIMIT 1
		) sum ON TRUE
		WHERE 1=1`
	args := []interface{}{includeDoctorNotes}
	idx := 2
	if clientID > 0 {
		q += fmt.Sprintf(" AND (cs.client_id = $%d OR cs.status = 'available')", idx)
		args = append(args, clientID)
		idx++
	}
	if statusFilter != "" {
		q += fmt.Sprintf(" AND cs.status = $%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	q += ` ORDER BY cs.slot_start ASC`

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []ConsultationSlot
	for rows.Next() {
		var sl ConsultationSlot
		if err := rows.Scan(
			&sl.ID, &sl.DoctorID, &sl.ClientID, &sl.OrderID, &sl.SlotStart, &sl.SlotEnd, &sl.Status,
			&sl.MeetingURL, &sl.ClientQuestion, &sl.DoctorNotes, &sl.CreatedAt, &sl.UpdatedAt,
			&sl.DoctorName, &sl.ClientName, &sl.ClientEmail, &sl.ClientPreg,
			&sl.RecordID, &sl.RecordStatus, &sl.SummaryID, &sl.SummaryStatus,
		); err != nil {
			return nil, err
		}
		res = append(res, sl)
	}
	return res, rows.Err()
}

// GetConsultationSlot fetches a single slot.
func (s *Store) GetConsultationSlot(ctx context.Context, slotID int64, includeNotes bool) (*ConsultationSlot, error) {
	var sl ConsultationSlot
	err := s.Pool.QueryRow(ctx, `
		SELECT cs.id, cs.doctor_id, cs.client_id, cs.order_id, cs.slot_start, cs.slot_end, cs.status,
		       COALESCE(cs.meeting_url,''), COALESCE(cs.client_question,''),
		       CASE WHEN $2 THEN COALESCE(cs.doctor_notes,'') ELSE '' END,
		       cs.created_at, cs.updated_at,
		       doc.full_name, COALESCE(cli.full_name,''), COALESCE(cli.email,''), COALESCE(cli.pregnancy_status,''),
		       cr.id, COALESCE(cr.status,''), sum.id, COALESCE(sum.status,'')
		FROM consultation_slots cs
		JOIN users doc ON doc.id = cs.doctor_id
		LEFT JOIN users cli ON cli.id = cs.client_id
		LEFT JOIN LATERAL (
			SELECT id, status FROM consultation_records WHERE slot_id = cs.id ORDER BY id DESC LIMIT 1
		) cr ON TRUE
		LEFT JOIN LATERAL (
			SELECT id, status FROM consultation_summaries WHERE slot_id = cs.id ORDER BY id DESC LIMIT 1
		) sum ON TRUE
		WHERE cs.id = $1`, slotID, includeNotes,
	).Scan(
		&sl.ID, &sl.DoctorID, &sl.ClientID, &sl.OrderID, &sl.SlotStart, &sl.SlotEnd, &sl.Status,
		&sl.MeetingURL, &sl.ClientQuestion, &sl.DoctorNotes, &sl.CreatedAt, &sl.UpdatedAt,
		&sl.DoctorName, &sl.ClientName, &sl.ClientEmail, &sl.ClientPreg,
		&sl.RecordID, &sl.RecordStatus, &sl.SummaryID, &sl.SummaryStatus,
	)
	if err != nil {
		return nil, err
	}
	return &sl, nil
}

// BookConsultationSlot atomically reserves an available slot for a client.
func (s *Store) BookConsultationSlot(ctx context.Context, slotID, clientID, orderID int64, question string) (*ConsultationSlot, error) {
	var sl ConsultationSlot
	err := s.Pool.QueryRow(ctx, `
		UPDATE consultation_slots
		SET client_id = $2, order_id = NULLIF($3, 0), status = 'booked', client_question = $4, updated_at = NOW()
		WHERE id = $1 AND status = 'available' AND slot_start > NOW() - INTERVAL '1 hour'
		RETURNING id, doctor_id, client_id, order_id, slot_start, slot_end, status, COALESCE(meeting_url,''), COALESCE(client_question,''), '', created_at, updated_at`,
		slotID, clientID, orderID, question,
	).Scan(&sl.ID, &sl.DoctorID, &sl.ClientID, &sl.OrderID, &sl.SlotStart, &sl.SlotEnd, &sl.Status, &sl.MeetingURL, &sl.ClientQuestion, &sl.DoctorNotes, &sl.CreatedAt, &sl.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("slot is no longer available")
		}
		return nil, err
	}
	return &sl, nil
}

// CancelConsultationSlot releases a booked slot back to available (if canceled by client) or canceled (by admin).
func (s *Store) CancelConsultationSlot(ctx context.Context, slotID, clientID int64, byAdmin bool) error {
	if byAdmin {
		_, err := s.Pool.Exec(ctx, `UPDATE consultation_slots SET status = 'canceled', updated_at = NOW() WHERE id = $1`, slotID)
		return err
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE consultation_slots
		SET client_id = NULL, order_id = NULL, status = 'available', client_question = NULL, updated_at = NOW()
		WHERE id = $1 AND client_id = $2 AND status = 'booked'`, slotID, clientID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("slot cannot be canceled")
	}
	return nil
}

// UpdateSlotQuestion updates client's pre-consultation complaints/question and invalidates any stale pre-consultation summary.
func (s *Store) UpdateSlotQuestion(ctx context.Context, slotID, clientID int64, question string) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE consultation_slots
		SET client_question = $3,
		    pre_summary_generated_at = CASE WHEN status = 'booked' THEN NULL ELSE pre_summary_generated_at END,
		    updated_at = NOW()
		WHERE id = $1 AND client_id = $2`, slotID, clientID, question)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// UpdateSlotAdmin allows doctor/admin to update slot times (with overlap check), meeting_url, status, and private doctor_notes.
func (s *Store) UpdateSlotAdmin(ctx context.Context, slotID int64, slotStart, slotEnd *time.Time, meetingURL, status, doctorNotes *string) error {
	if slotStart == nil && slotEnd == nil {
		_, err := s.Pool.Exec(ctx, `
			UPDATE consultation_slots
			SET meeting_url = COALESCE($2, meeting_url),
			    status = COALESCE($3, status),
			    doctor_notes = COALESCE($4, doctor_notes),
			    updated_at = NOW()
			WHERE id = $1`, slotID, meetingURL, status, doctorNotes)
		return err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var doctorID int64
	var curStart, curEnd time.Time
	if err := tx.QueryRow(ctx, `
		SELECT doctor_id, slot_start, slot_end
		FROM consultation_slots
		WHERE id = $1 FOR UPDATE`, slotID,
	).Scan(&doctorID, &curStart, &curEnd); err != nil {
		return err
	}

	effStart := curStart
	effEnd := curEnd
	if slotStart != nil {
		effStart = *slotStart
	}
	if slotEnd != nil {
		effEnd = *slotEnd
	}
	if err := validateSlotWindow(effStart, effEnd); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, doctorID); err != nil {
		return err
	}

	var conflictID int64
	var cStart, cEnd time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, slot_start, slot_end
		FROM consultation_slots
		WHERE doctor_id = $1
		  AND id <> $2
		  AND status <> 'canceled'
		  AND slot_start < $4
		  AND slot_end > $3
		ORDER BY slot_start ASC
		LIMIT 1`,
		doctorID, slotID, effStart, effEnd,
	).Scan(&conflictID, &cStart, &cEnd)
	if err == nil {
		return &SlotOverlapError{ConflictID: conflictID, ConflictStart: cStart, ConflictEnd: cEnd}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE consultation_slots
		SET slot_start = $2,
		    slot_end = $3,
		    meeting_url = COALESCE($4, meeting_url),
		    status = COALESCE($5, status),
		    doctor_notes = COALESCE($6, doctor_notes),
		    updated_at = NOW()
		WHERE id = $1`,
		slotID, effStart, effEnd, meetingURL, status, doctorNotes,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddConsultationAttachment records an uploaded lab/analysis file attached to a slot.
func (s *Store) AddConsultationAttachment(ctx context.Context, slotID, clientID int64, fileName, contentType string, size int64, s3Key string) (*ConsultationAttachment, error) {
	var att ConsultationAttachment
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO consultation_attachments (slot_id, client_id, file_name, content_type, file_size_bytes, s3_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, slot_id, client_id, file_name, content_type, file_size_bytes, s3_key, created_at`,
		slotID, clientID, fileName, contentType, size, s3Key,
	).Scan(&att.ID, &att.SlotID, &att.ClientID, &att.FileName, &att.ContentType, &att.FileSize, &att.S3Key, &att.CreatedAt)
	if err != nil {
		return nil, err
	}
	// Reset pre_summary_generated_at so any booked slot within 30 min regenerates its summary with the newly uploaded analysis
	_, _ = s.Pool.Exec(ctx, `UPDATE consultation_slots SET pre_summary_generated_at = NULL, updated_at = NOW() WHERE id = $1 AND status = 'booked'`, slotID)
	return &att, nil
}

// ListConsultationAttachments returns patient files attached to a consultation slot.
func (s *Store) ListConsultationAttachments(ctx context.Context, slotID int64) ([]ConsultationAttachment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, slot_id, client_id, file_name, content_type, file_size_bytes, s3_key, created_at
		FROM consultation_attachments WHERE slot_id = $1 ORDER BY created_at ASC`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ConsultationAttachment
	for rows.Next() {
		var a ConsultationAttachment
		if err := rows.Scan(&a.ID, &a.SlotID, &a.ClientID, &a.FileName, &a.ContentType, &a.FileSize, &a.S3Key, &a.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

// GetConsultationAttachment fetches one attachment by ID.
func (s *Store) GetConsultationAttachment(ctx context.Context, id int64) (*ConsultationAttachment, error) {
	var a ConsultationAttachment
	err := s.Pool.QueryRow(ctx, `
		SELECT id, slot_id, client_id, file_name, content_type, file_size_bytes, s3_key, created_at
		FROM consultation_attachments WHERE id = $1`, id,
	).Scan(&a.ID, &a.SlotID, &a.ClientID, &a.FileName, &a.ContentType, &a.FileSize, &a.S3Key, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateConsultationRecord saves metadata for an uploaded consultation audio file.
func (s *Store) CreateConsultationRecord(ctx context.Context, slotID, doctorID, clientID int64, sourceType, origName, s3Key string, size int64, audioFmt string) (*ConsultationRecord, error) {
	var r ConsultationRecord
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO consultation_records (slot_id, doctor_id, client_id, source_type, original_file_name, s3_audio_key, file_size_bytes, audio_format, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'uploaded')
		RETURNING id, slot_id, doctor_id, client_id, source_type, original_file_name, s3_audio_key, file_size_bytes, duration_seconds, audio_format, status, COALESCE(error_message,''), recorded_at, created_at`,
		slotID, doctorID, clientID, sourceType, origName, s3Key, size, audioFmt,
	).Scan(&r.ID, &r.SlotID, &r.DoctorID, &r.ClientID, &r.SourceType, &r.OriginalFile, &r.S3AudioKey, &r.FileSizeBytes, &r.DurationSeconds, &r.AudioFormat, &r.Status, &r.ErrorMessage, &r.RecordedAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetConsultationRecord fetches an audio record by ID.
func (s *Store) GetConsultationRecord(ctx context.Context, id int64) (*ConsultationRecord, error) {
	var r ConsultationRecord
	err := s.Pool.QueryRow(ctx, `
		SELECT id, slot_id, doctor_id, client_id, source_type, original_file_name, s3_audio_key, file_size_bytes, duration_seconds, audio_format, status, COALESCE(error_message,''), recorded_at, created_at
		FROM consultation_records WHERE id = $1`, id,
	).Scan(&r.ID, &r.SlotID, &r.DoctorID, &r.ClientID, &r.SourceType, &r.OriginalFile, &r.S3AudioKey, &r.FileSizeBytes, &r.DurationSeconds, &r.AudioFormat, &r.Status, &r.ErrorMessage, &r.RecordedAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListSlotRecords returns all audio records for a consultation slot.
func (s *Store) ListSlotRecords(ctx context.Context, slotID int64) ([]ConsultationRecord, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, slot_id, doctor_id, client_id, source_type, original_file_name, s3_audio_key, file_size_bytes, duration_seconds, audio_format, status, COALESCE(error_message,''), recorded_at, created_at
		FROM consultation_records WHERE slot_id = $1 ORDER BY created_at DESC`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ConsultationRecord
	for rows.Next() {
		var r ConsultationRecord
		if err := rows.Scan(&r.ID, &r.SlotID, &r.DoctorID, &r.ClientID, &r.SourceType, &r.OriginalFile, &r.S3AudioKey, &r.FileSizeBytes, &r.DurationSeconds, &r.AudioFormat, &r.Status, &r.ErrorMessage, &r.RecordedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

// ListTranscriptSegments returns Whisper transcript segments ordered by segment_index.
func (s *Store) ListTranscriptSegments(ctx context.Context, recordID int64) ([]TranscriptSegment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, record_id, segment_index, speaker_label, start_time_sec::float8, end_time_sec::float8, text, confidence::float8
		FROM consultation_transcripts WHERE record_id = $1 ORDER BY segment_index ASC`, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []TranscriptSegment
	for rows.Next() {
		var seg TranscriptSegment
		if err := rows.Scan(&seg.ID, &seg.RecordID, &seg.SegmentIndex, &seg.SpeakerLabel, &seg.StartTimeSec, &seg.EndTimeSec, &seg.Text, &seg.Confidence); err != nil {
			return nil, err
		}
		res = append(res, seg)
	}
	return res, rows.Err()
}

// GetSummaryByID fetches a consultation summary by ID.
func (s *Store) GetSummaryByID(ctx context.Context, id int64) (*ConsultationSummary, error) {
	var sum ConsultationSummary
	err := s.Pool.QueryRow(ctx, `
		SELECT id, record_id, slot_id, doctor_id, client_id, llm_model_name,
		       COALESCE(template_code,''), COALESCE(doctor_description,''),
		       COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
		       status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at
		FROM consultation_summaries WHERE id = $1`, id,
	).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
		&sum.TemplateCode, &sum.DoctorDescription,
		&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
		&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

// GetLatestSummaryForSlot fetches the most recent summary for a slot.
func (s *Store) GetLatestSummaryForSlot(ctx context.Context, slotID int64) (*ConsultationSummary, error) {
	var sum ConsultationSummary
	err := s.Pool.QueryRow(ctx, `
		SELECT id, record_id, slot_id, doctor_id, client_id, llm_model_name,
		       COALESCE(template_code,''), COALESCE(doctor_description,''),
		       COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
		       status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at
		FROM consultation_summaries WHERE slot_id = $1 ORDER BY id DESC LIMIT 1`, slotID,
	).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
		&sum.TemplateCode, &sum.DoctorDescription,
		&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
		&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sum, nil
}

// UpdateSummaryDraft saves doctor edits to a draft summary.
func (s *Store) UpdateSummaryDraft(ctx context.Context, id int64, templateCode, doctorDescription, complaints, anamnesis, recommendations, diagnosticsPlan, followUp, feedback string) (*ConsultationSummary, error) {
	var sum ConsultationSummary
	err := s.Pool.QueryRow(ctx, `
		UPDATE consultation_summaries
		SET template_code = $2,
		    doctor_description = $3,
		    complaints = $4,
		    anamnesis = $5,
		    recommendations = $6,
		    diagnostics_plan = $7,
		    follow_up = $8,
		    doctor_feedback = $9,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, record_id, slot_id, doctor_id, client_id, llm_model_name,
		          COALESCE(template_code,''), COALESCE(doctor_description,''),
		          COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
		          status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at`,
		id, templateCode, doctorDescription, complaints, anamnesis, recommendations, diagnosticsPlan, followUp, feedback,
	).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
		&sum.TemplateCode, &sum.DoctorDescription,
		&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
		&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

// ApproveSummary publishes a consultation summary PDF to patient_documents and marks the slot completed.
func (s *Store) ApproveSummary(ctx context.Context, summaryID int64, title, s3PDFKey string, pdfSize int64) (*ConsultationSummary, *PatientDocument, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var sum ConsultationSummary
	err = tx.QueryRow(ctx, `
		UPDATE consultation_summaries
		SET status = 'approved', approved_at = NOW(), updated_at = NOW()
		WHERE id = $1
		RETURNING id, record_id, slot_id, doctor_id, client_id, llm_model_name,
		          COALESCE(template_code,''), COALESCE(doctor_description,''),
		          COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
		          status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at`,
		summaryID,
	).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
		&sum.TemplateCode, &sum.DoctorDescription,
		&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
		&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}

	_, _ = tx.Exec(ctx, `UPDATE consultation_slots SET status = 'completed', updated_at = NOW() WHERE id = $1`, sum.SlotID)

	var doc PatientDocument
	// Avoid duplicate documents on repeated approval of the same summary
	err = tx.QueryRow(ctx, `
		SELECT id FROM patient_documents WHERE summary_id = $1 LIMIT 1`, sum.ID).Scan(&doc.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO patient_documents (client_id, slot_id, summary_id, title, document_type, s3_pdf_key, file_size_bytes, is_published, published_at)
			VALUES ($1, $2, $3, $4, 'consultation_summary', $5, $6, TRUE, NOW())
			RETURNING id, client_id, slot_id, summary_id, title, document_type, s3_pdf_key, file_size_bytes, is_published, published_at, created_at`,
			sum.ClientID, sum.SlotID, sum.ID, title, s3PDFKey, pdfSize,
		).Scan(&doc.ID, &doc.ClientID, &doc.SlotID, &doc.SummaryID, &doc.Title, &doc.DocumentType, &doc.S3PDFKey, &doc.FileSizeBytes, &doc.IsPublished, &doc.PublishedAt, &doc.CreatedAt)
	} else if err == nil {
		err = tx.QueryRow(ctx, `
			UPDATE patient_documents
			SET title = $2, s3_pdf_key = $3, file_size_bytes = $4, is_published = TRUE, published_at = NOW()
			WHERE id = $1
			RETURNING id, client_id, slot_id, summary_id, title, document_type, s3_pdf_key, file_size_bytes, is_published, published_at, created_at`,
			doc.ID, title, s3PDFKey, pdfSize,
		).Scan(&doc.ID, &doc.ClientID, &doc.SlotID, &doc.SummaryID, &doc.Title, &doc.DocumentType, &doc.S3PDFKey, &doc.FileSizeBytes, &doc.IsPublished, &doc.PublishedAt, &doc.CreatedAt)
	}
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &sum, &doc, nil
}

// ListPatientDocuments returns published consultation summaries and documents for a patient.
// Raw audio and draft transcripts are NEVER returned to the client.
func (s *Store) ListPatientDocuments(ctx context.Context, clientID int64, publishedOnly bool) ([]PatientDocument, error) {
	q := `
		SELECT pd.id, pd.client_id, pd.slot_id, pd.summary_id, pd.title, pd.document_type, pd.s3_pdf_key, pd.file_size_bytes, pd.is_published, pd.published_at, pd.created_at,
		       COALESCE(doc.full_name,''),
		       COALESCE(csum.complaints,''), COALESCE(csum.anamnesis,''), COALESCE(csum.recommendations,''), COALESCE(csum.diagnostics_plan,''), COALESCE(csum.follow_up,'')
		FROM patient_documents pd
		LEFT JOIN consultation_summaries csum ON csum.id = pd.summary_id
		LEFT JOIN users doc ON doc.id = csum.doctor_id
		WHERE pd.client_id = $1`
	if publishedOnly {
		q += ` AND pd.is_published = TRUE`
	}
	q += ` ORDER BY COALESCE(pd.published_at, pd.created_at) DESC`

	rows, err := s.Pool.Query(ctx, q, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []PatientDocument
	for rows.Next() {
		var d PatientDocument
		if err := rows.Scan(
			&d.ID, &d.ClientID, &d.SlotID, &d.SummaryID, &d.Title, &d.DocumentType, &d.S3PDFKey, &d.FileSizeBytes, &d.IsPublished, &d.PublishedAt, &d.CreatedAt,
			&d.DoctorName, &d.Complaints, &d.Anamnesis, &d.Recommendations, &d.DiagnosticsPlan, &d.FollowUp,
		); err != nil {
			return nil, err
		}
		res = append(res, d)
	}
	return res, rows.Err()
}

// GetPatientDocument fetches a single patient document.
func (s *Store) GetPatientDocument(ctx context.Context, docID int64) (*PatientDocument, error) {
	var d PatientDocument
	err := s.Pool.QueryRow(ctx, `
		SELECT id, client_id, slot_id, summary_id, title, document_type, s3_pdf_key, file_size_bytes, is_published, published_at, created_at
		FROM patient_documents WHERE id = $1`, docID,
	).Scan(&d.ID, &d.ClientID, &d.SlotID, &d.SummaryID, &d.Title, &d.DocumentType, &d.S3PDFKey, &d.FileSizeBytes, &d.IsPublished, &d.PublishedAt, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ConsultationReminderTarget represents a booked slot due for a 24h or 2h reminder.
type ConsultationReminderTarget struct {
	SlotID     int64
	ClientID   int64
	FullName   string
	Email      string
	MaxUserID  string
	SlotStart  time.Time
	MeetingURL string
	HoursWin   int // 24 or 2
}

// FindDueConsultationReminders returns booked future slots needing 24h or 2h notifications.
func (s *Store) FindDueConsultationReminders(ctx context.Context) ([]ConsultationReminderTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT cs.id, u.id, u.full_name, u.email, COALESCE(u.max_user_id,''), cs.slot_start, COALESCE(cs.meeting_url,''),
		       CASE
		           WHEN cs.slot_start <= NOW() + INTERVAL '2 hours' AND cs.reminder_2h_sent_at IS NULL THEN 2
		           WHEN cs.slot_start <= NOW() + INTERVAL '24 hours' AND cs.slot_start > NOW() + INTERVAL '2 hours' AND cs.reminder_24h_sent_at IS NULL THEN 24
		           ELSE 0
		       END AS win
		FROM consultation_slots cs
		JOIN users u ON u.id = cs.client_id
		WHERE cs.status = 'booked' AND cs.slot_start > NOW() AND cs.slot_start <= NOW() + INTERVAL '24 hours'
		  AND (
		      (cs.slot_start <= NOW() + INTERVAL '2 hours' AND cs.reminder_2h_sent_at IS NULL) OR
		      (cs.slot_start <= NOW() + INTERVAL '24 hours' AND cs.slot_start > NOW() + INTERVAL '2 hours' AND cs.reminder_24h_sent_at IS NULL)
		  )
		LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ConsultationReminderTarget
	for rows.Next() {
		var r ConsultationReminderTarget
		if err := rows.Scan(&r.SlotID, &r.ClientID, &r.FullName, &r.Email, &r.MaxUserID, &r.SlotStart, &r.MeetingURL, &r.HoursWin); err != nil {
			return nil, err
		}
		if r.HoursWin > 0 {
			res = append(res, r)
		}
	}
	return res, rows.Err()
}

// MarkConsultationReminderSent sets reminder_24h_sent_at or reminder_2h_sent_at.
func (s *Store) MarkConsultationReminderSent(ctx context.Context, slotID int64, hoursWin int) error {
	col := "reminder_24h_sent_at"
	if hoursWin == 2 {
		col = "reminder_2h_sent_at"
	}
	_, err := s.Pool.Exec(ctx, fmt.Sprintf(`UPDATE consultation_slots SET %s = NOW() WHERE id = $1`, col), slotID)
	return err
}

// ListSummaryTemplates returns all clinical conclusion templates ordered by sort_order.
func (s *Store) ListSummaryTemplates(ctx context.Context) ([]SummaryTemplate, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, code, title, category, keywords,
		       complaints_tpl, anamnesis_tpl, recommendations_tpl, diagnostics_tpl, follow_up_tpl
		FROM consultation_summary_templates
		ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []SummaryTemplate
	for rows.Next() {
		var t SummaryTemplate
		if err := rows.Scan(
			&t.ID, &t.Code, &t.Title, &t.Category, &t.Keywords,
			&t.ComplaintsTpl, &t.AnamnesisTpl, &t.RecommendationsTpl, &t.DiagnosticsTpl, &t.FollowUpTpl,
		); err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}

// GetSummaryTemplate returns one clinical template by its unique code.
func (s *Store) GetSummaryTemplate(ctx context.Context, code string) (*SummaryTemplate, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	var t SummaryTemplate
	err := s.Pool.QueryRow(ctx, `
		SELECT id, code, title, category, keywords,
		       complaints_tpl, anamnesis_tpl, recommendations_tpl, diagnostics_tpl, follow_up_tpl
		FROM consultation_summary_templates
		WHERE code = $1`, code,
	).Scan(
		&t.ID, &t.Code, &t.Title, &t.Category, &t.Keywords,
		&t.ComplaintsTpl, &t.AnamnesisTpl, &t.RecommendationsTpl, &t.DiagnosticsTpl, &t.FollowUpTpl,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// ListLatestTranscriptTextForSlot returns concatenated transcript text for the newest recording of a slot.
func (s *Store) ListLatestTranscriptTextForSlot(ctx context.Context, slotID int64) (string, error) {
	var recordID int64
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM consultation_records WHERE slot_id = $1 ORDER BY id DESC LIMIT 1`, slotID,
	).Scan(&recordID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	segs, err := s.ListTranscriptSegments(ctx, recordID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, seg := range segs {
		fmt.Fprintf(&b, "[%s] %s\n", seg.SpeakerLabel, seg.Text)
	}
	return b.String(), nil
}

// UpsertSlotSummaryDraft updates the latest summary for a slot or creates a manual/AI draft without requiring audio.
func (s *Store) UpsertSlotSummaryDraft(
	ctx context.Context,
	slotID, doctorID, clientID int64,
	llmModel, templateCode, doctorDescription, complaints, anamnesis, recommendations, diagnosticsPlan, followUp, feedback string,
) (*ConsultationSummary, error) {
	existing, err := s.GetLatestSummaryForSlot(ctx, slotID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(llmModel) == "" {
		llmModel = "doctor-manual"
	}
	if existing != nil {
		var sum ConsultationSummary
		err := s.Pool.QueryRow(ctx, `
			UPDATE consultation_summaries
			SET llm_model_name = $2,
			    template_code = $3,
			    doctor_description = $4,
			    complaints = $5,
			    anamnesis = $6,
			    recommendations = $7,
			    diagnostics_plan = $8,
			    follow_up = $9,
			    doctor_feedback = $10,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, record_id, slot_id, doctor_id, client_id, llm_model_name,
			          COALESCE(template_code,''), COALESCE(doctor_description,''),
			          COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
			          status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at`,
			existing.ID, llmModel, templateCode, doctorDescription, complaints, anamnesis, recommendations, diagnosticsPlan, followUp, feedback,
		).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
			&sum.TemplateCode, &sum.DoctorDescription,
			&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
			&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return &sum, nil
	}

	var sum ConsultationSummary
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO consultation_summaries (
			record_id, slot_id, doctor_id, client_id, llm_model_name,
			template_code, doctor_description, complaints, anamnesis, recommendations, diagnostics_plan, follow_up,
			status, doctor_feedback
		)
		VALUES (NULL, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'draft', $12)
		RETURNING id, record_id, slot_id, doctor_id, client_id, llm_model_name,
		          COALESCE(template_code,''), COALESCE(doctor_description,''),
		          COALESCE(complaints,''), COALESCE(anamnesis,''), recommendations, COALESCE(diagnostics_plan,''), COALESCE(follow_up,''),
		          status, COALESCE(doctor_feedback,''), approved_at, created_at, updated_at`,
		slotID, doctorID, clientID, llmModel, templateCode, doctorDescription, complaints, anamnesis, recommendations, diagnosticsPlan, followUp, feedback,
	).Scan(&sum.ID, &sum.RecordID, &sum.SlotID, &sum.DoctorID, &sum.ClientID, &sum.LLMModelName,
		&sum.TemplateCode, &sum.DoctorDescription,
		&sum.Complaints, &sum.Anamnesis, &sum.Recommendations, &sum.DiagnosticsPlan, &sum.FollowUp,
		&sum.Status, &sum.DoctorFeedback, &sum.ApprovedAt, &sum.CreatedAt, &sum.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

// GetClientProfile aggregates full history (materials, consultations, documents/prescriptions, orders, questions) for a patient.
func (s *Store) GetClientProfile(ctx context.Context, clientID int64) (*ClientProfile, error) {
	u, err := s.GetUserByID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	sub, _ := s.GetLatestSubscription(ctx, clientID)
	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())

	mats, err := s.ListCabinetMaterials(ctx, clientID)
	if err != nil {
		return nil, err
	}
	allSlots, err := s.ListConsultationSlots(ctx, true, clientID, "")
	if err != nil {
		return nil, err
	}
	var clientSlots []ConsultationSlot
	for _, sl := range allSlots {
		if sl.ClientID != nil && *sl.ClientID == clientID {
			clientSlots = append(clientSlots, sl)
		}
	}
	docs, err := s.ListPatientDocuments(ctx, clientID, false)
	if err != nil {
		return nil, err
	}
	orders, err := s.ListOrders(ctx, clientID, "")
	if err != nil {
		return nil, err
	}

	maxUID := ""
	if u.MaxUserID != nil {
		maxUID = strings.TrimSpace(*u.MaxUserID)
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, max_user_id, user_id, COALESCE(author_name,''), question_text, COALESCE(answer_text,''), status, created_at, answered_at
		FROM questions
		WHERE user_id = $1 OR ($2 <> '' AND max_user_id = $2)
		ORDER BY created_at DESC`, clientID, maxUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var qs []Question
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.MaxUserID, &q.UserID, &q.AuthorName, &q.QuestionText, &q.AnswerText, &q.Status, &q.CreatedAt, &q.AnsweredAt); err != nil {
			return nil, err
		}
		qs = append(qs, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	msgs, err := s.ListChatMessages(ctx, clientID)
	if err != nil {
		return nil, err
	}
	unreadDoc := 0
	for _, m := range msgs {
		if m.SenderRole == "client" && !m.ReadByDoctor {
			unreadDoc++
		}
	}

	atts, _ := s.ListClientAttachments(ctx, clientID)
	latestSum, _ := s.GetLatestPatientAISummary(ctx, clientID, "doctor")

	return &ClientProfile{
		User:            *u,
		Subscription:    sub,
		SubActive:       subActive,
		Materials:       mats,
		Consultations:   clientSlots,
		Attachments:     atts,
		Documents:       docs,
		Questions:       qs,
		Orders:          orders,
		Messages:        msgs,
		UnreadByDoctor:  unreadDoc,
		LatestAISummary: latestSum,
	}, nil
}

// ListClientAttachments returns all lab/analysis files uploaded by a patient across all consultation slots.
func (s *Store) ListClientAttachments(ctx context.Context, clientID int64) ([]ConsultationAttachment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, slot_id, client_id, file_name, content_type, file_size_bytes, s3_key, created_at
		FROM consultation_attachments
		WHERE client_id = $1
		ORDER BY created_at DESC, id DESC`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ConsultationAttachment
	for rows.Next() {
		var a ConsultationAttachment
		if err := rows.Scan(&a.ID, &a.SlotID, &a.ClientID, &a.FileName, &a.ContentType, &a.FileSize, &a.S3Key, &a.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

// SavePatientAISummary persists a generated patient profile summary (full, incremental, custom, or pre_consultation).
func (s *Store) SavePatientAISummary(ctx context.Context, sum PatientAISummary) (*PatientAISummary, error) {
	if sum.Audience == "" {
		sum.Audience = "doctor"
	}
	if sum.GenerationMode == "" {
		sum.GenerationMode = "full"
	}
	if sum.TemplateCode == "" {
		sum.TemplateCode = "avocado_clinical_v1"
	}
	if sum.ProcessedEventKeys == nil {
		sum.ProcessedEventKeys = []string{}
	}
	keysJSON, err := json.Marshal(sum.ProcessedEventKeys)
	if err != nil {
		keysJSON = []byte("[]")
	}
	if sum.CoveredUntilAt.IsZero() {
		sum.CoveredUntilAt = time.Now().UTC()
	}

	var out PatientAISummary
	var rawKeys []byte
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO patient_ai_summaries (
			client_id, slot_id, audience, generation_mode, template_code,
			scope_label, summary_text, processed_event_keys,
			events_count, new_events_count, base_summary_id, llm_model_name, covered_until_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13)
		RETURNING id, client_id, slot_id, audience, generation_mode, template_code,
		          scope_label, summary_text, processed_event_keys,
		          events_count, new_events_count, base_summary_id, llm_model_name, covered_until_at, created_at`,
		sum.ClientID, sum.SlotID, sum.Audience, sum.GenerationMode, sum.TemplateCode,
		sum.ScopeLabel, sum.SummaryText, string(keysJSON),
		sum.EventsCount, sum.NewEventsCount, sum.BaseSummaryID, sum.LLMModelName, sum.CoveredUntilAt,
	).Scan(
		&out.ID, &out.ClientID, &out.SlotID, &out.Audience, &out.GenerationMode, &out.TemplateCode,
		&out.ScopeLabel, &out.SummaryText, &rawKeys,
		&out.EventsCount, &out.NewEventsCount, &out.BaseSummaryID, &out.LLMModelName, &out.CoveredUntilAt, &out.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rawKeys, &out.ProcessedEventKeys)
	return &out, nil
}

// GetLatestPatientAISummary returns the most recent baseline/incremental summary for a patient and audience.
func (s *Store) GetLatestPatientAISummary(ctx context.Context, clientID int64, audience string) (*PatientAISummary, error) {
	if audience == "" {
		audience = "doctor"
	}
	var out PatientAISummary
	var rawKeys []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT id, client_id, slot_id, audience, generation_mode, template_code,
		       scope_label, summary_text, processed_event_keys,
		       events_count, new_events_count, base_summary_id, llm_model_name, covered_until_at, created_at
		FROM patient_ai_summaries
		WHERE client_id = $1 AND audience = $2
		  AND generation_mode IN ('full', 'incremental', 'pre_consultation')
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, clientID, audience,
	).Scan(
		&out.ID, &out.ClientID, &out.SlotID, &out.Audience, &out.GenerationMode, &out.TemplateCode,
		&out.ScopeLabel, &out.SummaryText, &rawKeys,
		&out.EventsCount, &out.NewEventsCount, &out.BaseSummaryID, &out.LLMModelName, &out.CoveredUntilAt, &out.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(rawKeys, &out.ProcessedEventKeys)
	return &out, nil
}

// GetPatientAISummaryForSlot returns the pre-consultation AI summary linked to a specific consultation slot.
func (s *Store) GetPatientAISummaryForSlot(ctx context.Context, slotID int64) (*PatientAISummary, error) {
	var out PatientAISummary
	var rawKeys []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT id, client_id, slot_id, audience, generation_mode, template_code,
		       scope_label, summary_text, processed_event_keys,
		       events_count, new_events_count, base_summary_id, llm_model_name, covered_until_at, created_at
		FROM patient_ai_summaries
		WHERE slot_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, slotID,
	).Scan(
		&out.ID, &out.ClientID, &out.SlotID, &out.Audience, &out.GenerationMode, &out.TemplateCode,
		&out.ScopeLabel, &out.SummaryText, &rawKeys,
		&out.EventsCount, &out.NewEventsCount, &out.BaseSummaryID, &out.LLMModelName, &out.CoveredUntilAt, &out.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(rawKeys, &out.ProcessedEventKeys)
	return &out, nil
}

// FindSlotsDueForPreConsultationSummary returns booked consultation slots starting within 30 minutes
// (e.g. slot at 10:00, triggered at 09:30) that have not yet had their pre-consultation summary generated.
func (s *Store) FindSlotsDueForPreConsultationSummary(ctx context.Context) ([]ConsultationSlot, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT cs.id, cs.doctor_id, cs.client_id, cs.order_id, cs.slot_start, cs.slot_end, cs.status,
		       COALESCE(cs.meeting_url,''), COALESCE(cs.client_question,''), COALESCE(cs.doctor_notes,''),
		       cs.created_at, cs.updated_at
		FROM consultation_slots cs
		WHERE cs.status = 'booked'
		  AND cs.client_id IS NOT NULL
		  AND cs.pre_summary_generated_at IS NULL
		  AND cs.slot_start <= NOW() + INTERVAL '30 minutes'
		  AND cs.slot_end >= NOW() - INTERVAL '30 minutes'
		ORDER BY cs.slot_start ASC
		LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ConsultationSlot
	for rows.Next() {
		var sl ConsultationSlot
		if err := rows.Scan(
			&sl.ID, &sl.DoctorID, &sl.ClientID, &sl.OrderID, &sl.SlotStart, &sl.SlotEnd, &sl.Status,
			&sl.MeetingURL, &sl.ClientQuestion, &sl.DoctorNotes, &sl.CreatedAt, &sl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, sl)
	}
	return res, rows.Err()
}

// MarkSlotPreSummaryGenerated records that the 30-minute pre-consultation summary has been generated for a slot.
func (s *Store) MarkSlotPreSummaryGenerated(ctx context.Context, slotID int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE consultation_slots SET pre_summary_generated_at = NOW() WHERE id = $1`, slotID)
	return err
}
