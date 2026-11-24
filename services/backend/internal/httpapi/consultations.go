package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/ai"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/pdf"
	"zdorovie/backend/internal/queue"
)

type chunkUploadMeta struct {
	UploadID    string `json:"upload_id"`
	SlotID      int64  `json:"slot_id"`
	DoctorID    int64  `json:"doctor_id"`
	FileName    string `json:"file_name"`
	AudioFormat string `json:"audio_format"`
	SourceType  string `json:"source_type"`
}

func isAllowedAudioFormat(ext string) (string, string, bool) {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	switch ext {
	case "mp3":
		return "mp3", "audio/mpeg", true
	case "m4a", "mp4":
		return "m4a", "audio/mp4", true
	case "wav":
		return "wav", "audio/wav", true
	case "aac":
		return "aac", "audio/aac", true
	case "ogg", "oga":
		return "ogg", "audio/ogg", true
	case "webm":
		return "webm", "audio/webm", true
	default:
		return "", "", false
	}
}

func (s *Server) handleDoctorSlots(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	slots, err := s.store.ListConsultationSlots(r.Context(), true, 0, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"slots": slots})
}

func (s *Server) handleSlotWorkspace(w http.ResponseWriter, r *http.Request) {
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Слот консультации не найден")
		return
	}
	atts, _ := s.store.ListConsultationAttachments(r.Context(), slotID)
	records, _ := s.store.ListSlotRecords(r.Context(), slotID)
	summary, _ := s.store.GetLatestSummaryForSlot(r.Context(), slotID)
	templates, _ := s.store.ListSummaryTemplates(r.Context())

	var transcripts []db.TranscriptSegment
	var transcriptText string
	if len(records) > 0 {
		transcripts, _ = s.store.ListTranscriptSegments(r.Context(), records[0].ID)
		var b strings.Builder
		for _, seg := range transcripts {
			b.WriteString(seg.Text)
			b.WriteByte(' ')
		}
		transcriptText = b.String()
	}

	ctxHints := strings.TrimSpace(sl.ClientQuestion + "\n" + sl.DoctorNotes + "\n" + transcriptText)
	if summary != nil {
		ctxHints = strings.TrimSpace(summary.DoctorDescription + "\n" + summary.Complaints + "\n" + ctxHints)
	}
	recCode, recReason := ai.RecommendTemplate(r.Context(), s.cfg, templates, ai.MaskPII(ctxHints, sl.ClientName), sl.ClientPreg)

	var preConsultSummary *db.PatientAISummary
	var patientLatestSummary *db.PatientAISummary
	unprocessedEventsCount := 0
	if sl.ClientID != nil && *sl.ClientID > 0 {
		preConsultSummary, _ = s.store.GetPatientAISummaryForSlot(r.Context(), slotID)
		if profile, pErr := s.store.GetClientProfile(r.Context(), *sl.ClientID); pErr == nil && profile != nil {
			// If slot starts within 30 minutes (or is currently active) and has no pre-consultation summary yet, build it automatically.
			if preConsultSummary == nil && sl.Status == "booked" &&
				!sl.SlotStart.After(time.Now().UTC().Add(30*time.Minute)) &&
				!sl.SlotEnd.Before(time.Now().UTC().Add(-30*time.Minute)) {
				_, saved, _, genErr := s.generateAndPersistPatientSummary(
					r.Context(),
					profile,
					ai.SummaryFilterRequest{Mode: "pre_consultation", SlotID: &slotID},
					sl,
					false,
				)
				if genErr == nil && saved != nil {
					preConsultSummary = saved
					profile.LatestAISummary = saved
				}
			}
			patientLatestSummary = profile.LatestAISummary
			unprocessedEventsCount = ai.CountUnprocessedProfileEvents(profile, patientLatestSummary, false)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"slot":                      sl,
		"attachments":               atts,
		"records":                   records,
		"transcript":                transcripts,
		"summary":                   summary,
		"templates":                 templates,
		"recommended_template_code": recCode,
		"recommended_reason":        recReason,
		"pre_consultation_summary":  preConsultSummary,
		"patient_latest_summary":    patientLatestSummary,
		"unprocessed_events_count":  unprocessedEventsCount,
		"summary_template":          ai.GetPatientSummaryTemplateSpec(),
	})
}

func (s *Server) handleUploadAudio(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, false)
	if err != nil || sl.ClientID == nil {
		writeError(w, http.StatusBadRequest, "invalid_slot", "Слот не найден или за ним не закреплён пациент")
		return
	}

	// Support streaming multipart audio up to 500 MB without blowing up RAM (32MB buffer to disk)
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "upload_error", "Ошибка чтения аудиофайла (лимит 500 МБ)")
		return
	}
	file, hdr, err := r.FormFile("audio")
	if err != nil {
		file, hdr, err = r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing_audio", "Ожидается поле формы 'audio' или 'file'")
			return
		}
	}
	defer file.Close()

	sourceType := strings.TrimSpace(r.FormValue("source_type"))
	if sourceType != "desktop_recorder" {
		sourceType = "manual_upload"
	}

	audioFmt, contentType, ok := isAllowedAudioFormat(filepath.Ext(hdr.Filename))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported_audio", "Поддерживаются только mp3, m4a, wav, aac, ogg, webm")
		return
	}

	safeName := filepath.Base(hdr.Filename)
	s3Key := fmt.Sprintf("audio/slot_%d/record_%d.%s", slotID, time.Now().Unix(), audioFmt)
	if err := s.s3.PutStream(r.Context(), s.s3.BucketConsult, s3Key, file, hdr.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Ошибка загрузки аудио в S3")
		return
	}

	rec, err := s.store.CreateConsultationRecord(r.Context(), slotID, actor.ID, *sl.ClientID, sourceType, safeName, s3Key, hdr.Size, audioFmt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка сохранения записи в БД")
		return
	}

	if s.queue != nil {
		_ = s.queue.EnqueueAudioTranscribe(r.Context(), queue.AudioTranscribePayload{
			RecordID:   rec.ID,
			SlotID:     slotID,
			DoctorID:   actor.ID,
			ClientID:   *sl.ClientID,
			S3AudioKey: s3Key,
			Format:     audioFmt,
		})
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "consultation.audio_uploaded", "consultation_record", strconv.FormatInt(rec.ID, 10), map[string]interface{}{
		"slot_id":     slotID,
		"source_type": sourceType,
		"size_bytes":  hdr.Size,
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"record": rec})
}

func chunkDir(uploadID string) string {
	return filepath.Join(os.TempDir(), "zdorovie_chunks", uploadID)
}

func (s *Server) handleChunkUploadInit(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	var req struct {
		SlotID      int64  `json:"slot_id"`
		FileName    string `json:"file_name"`
		AudioFormat string `json:"audio_format"`
		SourceType  string `json:"source_type"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), req.SlotID, false)
	if err != nil || sl.ClientID == nil {
		writeError(w, http.StatusBadRequest, "invalid_slot", "Слот не найден или не забронирован")
		return
	}

	fmtCandidate := req.AudioFormat
	if fmtCandidate == "" {
		fmtCandidate = filepath.Ext(req.FileName)
	}
	audioFmt, _, ok := isAllowedAudioFormat(fmtCandidate)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported_audio", "Неподдерживаемый аудиоформат")
		return
	}
	if req.SourceType != "manual_upload" {
		req.SourceType = "desktop_recorder"
	}

	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	uploadID := hex.EncodeToString(rnd)

	dir := chunkDir(uploadID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "fs_error", "Ошибка подготовки буфера загрузки")
		return
	}
	meta := chunkUploadMeta{
		UploadID:    uploadID,
		SlotID:      req.SlotID,
		DoctorID:    actor.ID,
		FileName:    filepath.Base(req.FileName),
		AudioFormat: audioFmt,
		SourceType:  req.SourceType,
	}
	rawMeta, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), rawMeta, 0o600)

	writeJSON(w, http.StatusCreated, map[string]interface{}{"upload_id": uploadID})
}

func (s *Server) handleChunkUploadPart(w http.ResponseWriter, r *http.Request) {
	uploadID := r.PathValue("uploadId")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if uploadID == "" || strings.Contains(uploadID, "..") || strings.Contains(uploadID, "/") || err != nil || idx < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректные параметры чанка")
		return
	}
	dir := chunkDir(uploadID)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Сессия загрузки не найдена")
		return
	}

	partPath := filepath.Join(dir, fmt.Sprintf("part_%06d", idx))
	f, err := os.Create(partPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fs_error", err.Error())
		return
	}
	written, copyErr := io.Copy(f, http.MaxBytesReader(w, r.Body, 50<<20))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "chunk_error", "Ошибка записи чанка")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"chunk": idx, "bytes": written})
}

func (s *Server) handleChunkUploadComplete(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	uploadID := r.PathValue("uploadId")
	if uploadID == "" || strings.Contains(uploadID, "..") || strings.Contains(uploadID, "/") {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный uploadId")
		return
	}
	dir := chunkDir(uploadID)
	rawMeta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Сессия загрузки не найдена")
		return
	}
	var meta chunkUploadMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		writeError(w, http.StatusInternalServerError, "meta_error", err.Error())
		return
	}
	defer os.RemoveAll(dir)

	sl, err := s.store.GetConsultationSlot(r.Context(), meta.SlotID, false)
	if err != nil || sl.ClientID == nil {
		writeError(w, http.StatusBadRequest, "invalid_slot", "Слот не найден")
		return
	}

	parts, err := filepath.Glob(filepath.Join(dir, "part_*"))
	if err != nil || len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "no_chunks", "Не загружено ни одного фрагмента")
		return
	}
	sort.Strings(parts)

	mergedPath := filepath.Join(dir, "merged."+meta.AudioFormat)
	outFile, err := os.Create(mergedPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fs_error", err.Error())
		return
	}
	var totalBytes int64
	for _, p := range parts {
		pf, openErr := os.Open(p)
		if openErr != nil {
			outFile.Close()
			writeError(w, http.StatusInternalServerError, "fs_error", openErr.Error())
			return
		}
		n, _ := io.Copy(outFile, pf)
		pf.Close()
		totalBytes += n
	}
	_ = outFile.Close()

	mergedReader, err := os.Open(mergedPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fs_error", err.Error())
		return
	}
	defer mergedReader.Close()

	_, contentType, _ := isAllowedAudioFormat(meta.AudioFormat)
	s3Key := fmt.Sprintf("audio/slot_%d/record_%d.%s", meta.SlotID, time.Now().Unix(), meta.AudioFormat)
	if err := s.s3.PutStream(r.Context(), s.s3.BucketConsult, s3Key, mergedReader, totalBytes, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Ошибка сохранения в MinIO")
		return
	}

	rec, err := s.store.CreateConsultationRecord(r.Context(), meta.SlotID, actor.ID, *sl.ClientID, meta.SourceType, meta.FileName, s3Key, totalBytes, meta.AudioFormat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if s.queue != nil {
		_ = s.queue.EnqueueAudioTranscribe(r.Context(), queue.AudioTranscribePayload{
			RecordID:   rec.ID,
			SlotID:     meta.SlotID,
			DoctorID:   actor.ID,
			ClientID:   *sl.ClientID,
			S3AudioKey: s3Key,
			Format:     meta.AudioFormat,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"record": rec})
}

func (s *Server) handleAudioPresignedURL(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	recID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID записи")
		return
	}
	rec, err := s.store.GetConsultationRecord(r.Context(), recID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Запись не найдена")
		return
	}
	// Strictly 5 minutes (300 seconds) TTL for audio playback per security spec
	url, err := s.s3.PresignedGetURL(r.Context(), s.s3.BucketConsult, rec.S3AudioKey, 300*time.Second, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", err.Error())
		return
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "consultation.audio_accessed", "consultation_record", strconv.FormatInt(recID, 10), nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":             url,
		"expires_seconds": 300,
		"audio_format":    rec.AudioFormat,
	})
}

func (s *Server) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	recID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID записи")
		return
	}
	rec, err := s.store.GetConsultationRecord(r.Context(), recID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Запись не найдена")
		return
	}
	segs, err := s.store.ListTranscriptSegments(r.Context(), recID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"record":   rec,
		"segments": segs,
	})
}

func (s *Server) handleRetranscribe(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	recID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID записи")
		return
	}
	rec, err := s.store.GetConsultationRecord(r.Context(), recID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Запись не найдена")
		return
	}
	if s.queue != nil {
		if err := s.queue.EnqueueAudioTranscribe(r.Context(), queue.AudioTranscribePayload{
			RecordID:   rec.ID,
			SlotID:     rec.SlotID,
			DoctorID:   actor.ID,
			ClientID:   rec.ClientID,
			S3AudioKey: rec.S3AudioKey,
			Format:     rec.AudioFormat,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "queue_error", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
}

func (s *Server) handleUpdateSummaryDraft(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID саммари")
		return
	}
	var req struct {
		TemplateCode      string `json:"template_code"`
		DoctorDescription string `json:"doctor_description"`
		Complaints        string `json:"complaints"`
		Anamnesis         string `json:"anamnesis"`
		Recommendations   string `json:"recommendations"`
		DiagnosticsPlan   string `json:"diagnostics_plan"`
		FollowUp          string `json:"follow_up"`
		DoctorFeedback    string `json:"doctor_feedback"`
	}
	if err := decodeJSON(r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	sum, err := s.store.UpdateSummaryDraft(r.Context(), id, req.TemplateCode, req.DoctorDescription, req.Complaints, req.Anamnesis, req.Recommendations, req.DiagnosticsPlan, req.FollowUp, req.DoctorFeedback)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Саммари не найдено")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"summary": sum})
}

func (s *Server) handleUpsertSlotSummary(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Слот консультации не найден")
		return
	}
	if sl.ClientID == nil {
		writeError(w, http.StatusBadRequest, "slot_without_client", "За слотом ещё не закреплена пациентка")
		return
	}
	var req struct {
		TemplateCode      string `json:"template_code"`
		DoctorDescription string `json:"doctor_description"`
		Complaints        string `json:"complaints"`
		Anamnesis         string `json:"anamnesis"`
		Recommendations   string `json:"recommendations"`
		DiagnosticsPlan   string `json:"diagnostics_plan"`
		FollowUp          string `json:"follow_up"`
		DoctorFeedback    string `json:"doctor_feedback"`
	}
	if err := decodeJSON(r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	if strings.TrimSpace(req.Recommendations) == "" && strings.TrimSpace(req.DoctorDescription) == "" && strings.TrimSpace(req.Complaints) == "" {
		writeError(w, http.StatusBadRequest, "empty_summary", "Заполните описание врача, рекомендации или жалобы")
		return
	}
	recs := req.Recommendations
	if strings.TrimSpace(recs) == "" {
		recs = req.DoctorDescription
	}
	sum, err := s.store.UpsertSlotSummaryDraft(
		r.Context(), slotID, actor.ID, *sl.ClientID, "doctor-manual",
		req.TemplateCode, req.DoctorDescription, req.Complaints, req.Anamnesis, recs, req.DiagnosticsPlan, req.FollowUp, req.DoctorFeedback,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"summary": sum})
}

func (s *Server) handleAIDraftSummary(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Слот консультации не найден")
		return
	}
	if sl.ClientID == nil {
		writeError(w, http.StatusBadRequest, "slot_without_client", "Слот ещё не забронирован пациенткой — сначала закрепите консультацию за пациенткой")
		return
	}

	var req struct {
		TemplateCode      string `json:"template_code"`
		DoctorDescription string `json:"doctor_description"`
		DoctorFeedback    string `json:"doctor_feedback"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req, 256<<10); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
			return
		}
	}

	templates, _ := s.store.ListSummaryTemplates(r.Context())
	transcriptText, _ := s.store.ListLatestTranscriptTextForSlot(r.Context(), slotID)
	existing, _ := s.store.GetLatestSummaryForSlot(r.Context(), slotID)

	docDesc := strings.TrimSpace(req.DoctorDescription)
	if docDesc == "" && existing != nil {
		docDesc = strings.TrimSpace(existing.DoctorDescription)
	}
	tplCode := strings.TrimSpace(req.TemplateCode)
	recReason := ""
	if tplCode == "" {
		hints := strings.TrimSpace(docDesc + "\n" + sl.ClientQuestion + "\n" + sl.DoctorNotes + "\n" + transcriptText)
		tplCode, recReason = ai.RecommendTemplate(r.Context(), s.cfg, templates, ai.MaskPII(hints, sl.ClientName), sl.ClientPreg)
	}
	tpl, _ := s.store.GetSummaryTemplate(r.Context(), tplCode)

	gen, err := ai.GenerateConclusionFromDescription(
		r.Context(), s.cfg, tpl, docDesc, transcriptText, sl.ClientQuestion, sl.ClientPreg, sl.ClientName,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_error", "Не удалось сгенерировать черновик ИИ")
		return
	}

	feedback := req.DoctorFeedback
	if feedback == "" && existing != nil {
		feedback = existing.DoctorFeedback
	}
	sum, err := s.store.UpsertSlotSummaryDraft(
		r.Context(), slotID, actor.ID, *sl.ClientID, gen.LLMModelName,
		gen.TemplateCode, docDesc, gen.Complaints, gen.Anamnesis, gen.Recommendations, gen.DiagnosticsPlan, gen.FollowUp, feedback,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "summary.ai_draft", "consultation_summary", strconv.FormatInt(sum.ID, 10), map[string]interface{}{
		"model":         gen.LLMModelName,
		"template_code": gen.TemplateCode,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":                   sum,
		"generated":                 gen,
		"recommended_template_code": gen.TemplateCode,
		"recommended_reason":        recReason,
	})
}

func (s *Server) handleApproveSummary(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID саммари")
		return
	}

	// Allow submitting final inline edits alongside approval
	var body struct {
		TemplateCode      string `json:"template_code"`
		DoctorDescription string `json:"doctor_description"`
		Complaints        string `json:"complaints"`
		Anamnesis         string `json:"anamnesis"`
		Recommendations   string `json:"recommendations"`
		DiagnosticsPlan   string `json:"diagnostics_plan"`
		FollowUp          string `json:"follow_up"`
		DoctorFeedback    string `json:"doctor_feedback"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body, 256<<10); err == nil && (body.Recommendations != "" || body.Complaints != "" || body.Anamnesis != "" || body.DiagnosticsPlan != "" || body.FollowUp != "" || body.DoctorFeedback != "" || body.DoctorDescription != "" || body.TemplateCode != "") {
			_, _ = s.store.UpdateSummaryDraft(r.Context(), id, body.TemplateCode, body.DoctorDescription, body.Complaints, body.Anamnesis, body.Recommendations, body.DiagnosticsPlan, body.FollowUp, body.DoctorFeedback)
		}
	}

	sum, err := s.store.GetSummaryByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Саммари не найдено")
		return
	}
	patient, err := s.store.GetUserByID(r.Context(), sum.ClientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Пациент не найден")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), sum.SlotID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Слот не найден")
		return
	}

	approvedAt := time.Now().UTC()
	pdfBytes, err := pdf.GenerateConsultationSummary(pdf.SummaryInput{
		SummaryID:       sum.ID,
		DoctorName:      actor.FullName,
		PatientName:     patient.FullName,
		PatientEmail:    patient.Email,
		PatientPhone:    patient.Phone,
		PregnancyStatus: patient.PregnancyStatus,
		ConsultDate:     sl.SlotStart,
		ApprovedAt:      approvedAt,
		Complaints:      sum.Complaints,
		Anamnesis:       sum.Anamnesis,
		Recommendations: sum.Recommendations,
		DiagnosticsPlan: sum.DiagnosticsPlan,
		FollowUp:        sum.FollowUp,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pdf_error", "Ошибка генерации PDF-заключения: "+err.Error())
		return
	}

	s3Key := fmt.Sprintf("summaries/%d/consultation_%d_summary_%d.pdf", patient.ID, sum.SlotID, sum.ID)
	if err := s.s3.PutBytes(r.Context(), s.s3.BucketConsult, s3Key, pdfBytes, "application/pdf"); err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Не удалось сохранить PDF-заключение в S3")
		return
	}

	title := fmt.Sprintf("Итоговые рекомендации от %s", sl.SlotStart.In(time.FixedZone("MSK", 3*3600)).Format("02.01.2006"))
	approvedSum, doc, err := s.store.ApproveSummary(r.Context(), sum.ID, title, s3Key, int64(len(pdfBytes)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	docsURL := strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/cabinet/documents"
	if s.yoo != nil {
		s.yoo.DeliverEmail(patient.Email, "Готовы рекомендации по вашей консультации", fmt.Sprintf(
			"Здравствуйте, %s!\n\nВрач утвердил итоговые рекомендации по вашей консультации (%s).\nПерсональный PDF-документ доступен в разделе «Мои консультации и документы»:\n%s\n",
			patient.FullName, title, docsURL,
		))
	}
	if patient.MaxUserID != nil && *patient.MaxUserID != "" && s.bot != nil {
		var rows [][]maxbot.Button
		rows = append(rows, []maxbot.Button{{
			Type:    "callback",
			Text:    "📋 Открыть заключение прямо в боте",
			Payload: fmt.Sprintf("doc:view:%d", doc.ID),
		}})
		if s.s3 != nil && s3Key != "" {
			filename := fmt.Sprintf("recommendations_%d.pdf", doc.ID)
			if pdfURL, pErr := s.s3.PresignedGetURL(r.Context(), s.s3.BucketConsult, s3Key, 30*time.Minute, filename); pErr == nil && pdfURL != "" {
				rows = append(rows, []maxbot.Button{{Type: "link", Text: "📥 Скачать PDF-заключение", URL: pdfURL}})
			}
		}
		rows = append(rows, []maxbot.Button{{Type: "link", Text: "🌐 Веб-кабинет", URL: docsURL}})
		_ = s.bot.SendMessage(r.Context(), *patient.MaxUserID, maxbot.OutgoingMessage{
			Text: fmt.Sprintf(
				"📋 *Готовы итоги вашей консультации!*\n\n"+
					"Врач утвердил и опубликовал медицинские рекомендации (*%s*).\n\n"+
					"• *Рекомендации:* %s\n\n"+
					"Нажмите кнопку ниже, чтобы прочитать полное заключение или скачать официальный PDF-документ прямо здесь в боте:",
				maxbot.EscapeMarkdown(title),
				maxbot.EscapeMarkdown(approvedSum.Recommendations),
			),
			Attachments: maxbot.InlineKeyboard(rows...),
		})
	}

	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "consultation.summary_approved", "consultation_summary", strconv.FormatInt(sum.ID, 10), map[string]interface{}{
		"document_id": doc.ID,
		"s3_pdf_key":  s3Key,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":  approvedSum,
		"document": doc,
	})
}
