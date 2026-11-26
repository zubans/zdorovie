package httpapi

import (
	"context"
	"time"

	"zdorovie/backend/internal/ai"
	"zdorovie/backend/internal/db"
)

type summaryFilterRequest = ai.SummaryFilterRequest

func buildPatientAIContext(profile *db.ClientProfile, req summaryFilterRequest, patientSafe bool) (ai.PatientContextForAI, error) {
	var prev *db.PatientAISummary
	if profile != nil {
		prev = profile.LatestAISummary
	}
	return ai.BuildPatientAIContext(profile, req, prev, nil, patientSafe)
}

func (s *Server) generateAndPersistPatientSummary(
	ctx context.Context,
	profile *db.ClientProfile,
	req ai.SummaryFilterRequest,
	targetSlot *db.ConsultationSlot,
	patientSafe bool,
) (ai.PatientSummaryOutput, *db.PatientAISummary, int, error) {
	audience := "doctor"
	if patientSafe {
		audience = "patient"
	}

	var prev *db.PatientAISummary
	if s.store != nil && profile != nil {
		prev, _ = s.store.GetLatestPatientAISummary(ctx, profile.User.ID, audience)
	}

	if targetSlot == nil && req.SlotID != nil && *req.SlotID > 0 && s.store != nil {
		targetSlot, _ = s.store.GetConsultationSlot(ctx, *req.SlotID, !patientSafe)
	}

	pCtx, err := ai.BuildPatientAIContext(profile, req, prev, targetSlot, patientSafe)
	if err != nil {
		return ai.PatientSummaryOutput{}, nil, 0, err
	}

	aiOut, err := ai.GeneratePatientSummary(ctx, s.cfg, pCtx)
	if err != nil {
		return ai.PatientSummaryOutput{}, nil, 0, err
	}

	var slotIDPtr *int64
	if targetSlot != nil {
		slotIDPtr = &targetSlot.ID
	} else if req.SlotID != nil && *req.SlotID > 0 {
		slotIDPtr = req.SlotID
	}

	var saved *db.PatientAISummary
	if s.store != nil && profile != nil && profile.User.ID > 0 {
		rec := db.PatientAISummary{
			ClientID:           profile.User.ID,
			SlotID:             slotIDPtr,
			Audience:           audience,
			GenerationMode:     pCtx.GenerationMode,
			TemplateCode:       aiOut.TemplateCode,
			ScopeLabel:         aiOut.ScopeLabel,
			SummaryText:        aiOut.SummaryText,
			ProcessedEventKeys: aiOut.ProcessedEventKeys,
			EventsCount:        aiOut.EventsCount,
			NewEventsCount:     aiOut.NewEventsCount,
			BaseSummaryID:      aiOut.BaseSummaryID,
			LLMModelName:       aiOut.LLMModelName,
			CoveredUntilAt:     time.Now().UTC(),
		}
		if persisted, saveErr := s.store.SavePatientAISummary(ctx, rec); saveErr == nil {
			saved = persisted
			aiOut.ID = persisted.ID
			aiOut.SlotID = persisted.SlotID
			aiOut.CreatedAt = &persisted.CreatedAt
			if slotIDPtr != nil && pCtx.GenerationMode == "pre_consultation" {
				_ = s.store.MarkSlotPreSummaryGenerated(ctx, *slotIDPtr)
			}
		}
	}

	unprocessedAfter := 0
	if saved != nil {
		unprocessedAfter = ai.CountUnprocessedProfileEvents(profile, saved, patientSafe)
	}
	return aiOut, saved, unprocessedAfter, nil
}

// ensureDuePreConsultationSummary checks if the patient has an upcoming booked slot within 30 minutes
// that hasn't had its pre-consultation summary generated yet, and generates it immediately if due.
func (s *Server) ensureDuePreConsultationSummary(ctx context.Context, profile *db.ClientProfile) *db.PatientAISummary {
	if s.store == nil || profile == nil {
		return nil
	}
	now := time.Now().UTC()
	windowEnd := now.Add(30 * time.Minute)
	windowStart := now.Add(-30 * time.Minute)

	for i := range profile.Consultations {
		sl := &profile.Consultations[i]
		if sl.Status != "booked" {
			continue
		}
		if sl.SlotStart.After(windowEnd) || sl.SlotEnd.Before(windowStart) {
			continue
		}
		existing, _ := s.store.GetPatientAISummaryForSlot(ctx, sl.ID)
		if existing != nil {
			return existing
		}
		_, saved, _, err := s.generateAndPersistPatientSummary(
			ctx,
			profile,
			ai.SummaryFilterRequest{Mode: "pre_consultation", SlotID: &sl.ID},
			sl,
			false,
		)
		if err == nil && saved != nil {
			profile.LatestAISummary = saved
			return saved
		}
	}
	return nil
}
