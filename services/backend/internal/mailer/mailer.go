package mailer

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"sync"

	"zdorovie/backend/internal/config"
)

// Message represents an outbound email notification.
type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Sender sends email notifications.
type Sender interface {
	Send(to, subject, body string) error
}

// Service implements Sender with smtp or mock mode.
type Service struct {
	cfg  config.Config
	mu   sync.Mutex
	sent []Message
}

// New creates a new email service.
func New(cfg config.Config) *Service {
	return &Service{cfg: cfg}
}

// Send delivers an email via SMTP or logs it in mock mode.
func (s *Service) Send(to, subject, body string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("recipient email is empty")
	}
	if s.cfg.EmailMode != "smtp" || s.cfg.SMTPHost == "" {
		s.mu.Lock()
		s.sent = append(s.sent, Message{To: to, Subject: subject, Body: body})
		if len(s.sent) > 100 {
			s.sent = s.sent[len(s.sent)-100:]
		}
		s.mu.Unlock()
		log.Printf("[Email Mock] to=%s subject=%q", to, subject)
		return nil
	}

	addr := s.cfg.SMTPHost + ":" + s.cfg.SMTPPort
	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}
	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.cfg.SMTPFrom, to, subject, body)
	return smtp.SendMail(addr, auth, s.cfg.SMTPFrom, []string{to}, []byte(raw))
}

// Recent returns recent mocked emails for local development inspection.
func (s *Service) Recent() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.sent))
	copy(out, s.sent)
	return out
}
