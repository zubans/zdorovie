package db

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateSlotWindowAndOverlapError(t *testing.T) {
	base := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if err := validateSlotWindow(base, base); !errors.Is(err, ErrInvalidSlotDuration) {
		t.Fatalf("expected ErrInvalidSlotDuration for zero-length slot, got %v", err)
	}
	if err := validateSlotWindow(base, base.Add(10*time.Minute)); !errors.Is(err, ErrInvalidSlotDuration) {
		t.Fatalf("expected ErrInvalidSlotDuration for <15m slot, got %v", err)
	}
	if err := validateSlotWindow(base, base.Add(245*time.Minute)); !errors.Is(err, ErrInvalidSlotDuration) {
		t.Fatalf("expected ErrInvalidSlotDuration for >240m slot, got %v", err)
	}
	if err := validateSlotWindow(base, base.Add(45*time.Minute)); err != nil {
		t.Fatalf("expected valid 45m slot to pass, got %v", err)
	}

	ov := &SlotOverlapError{
		ConflictID:    42,
		ConflictStart: base,
		ConflictEnd:   base.Add(45 * time.Minute),
	}
	if !errors.Is(ov, ErrSlotOverlap) {
		t.Fatalf("expected SlotOverlapError to unwrap to ErrSlotOverlap")
	}
	if !strings.Contains(ov.Error(), "#42") {
		t.Fatalf("expected error message to reference conflict id #42, got %q", ov.Error())
	}
}
