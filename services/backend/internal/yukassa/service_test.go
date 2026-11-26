package yukassa

import (
	"net/http"
	"testing"

	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
)

func TestBuildCreatePaymentPayload_NPDReceipt(t *testing.T) {
	svc := NewService(config.Config{YooReturnURL: "https://example.com/ret"}, nil, nil, nil, nil)
	payload := svc.BuildCreatePaymentPayload(
		db.Order{ID: 5, OrderNumber: "ZDOR-1005", AmountKopecks: 999000},
		db.User{ID: 12, FullName: "Мария Петрова", Email: "maria@example.com", Phone: "+79990001122"},
		db.Tariff{Code: "feedback_3m", Title: "С обратной связью 3 мес."},
	)

	if payload["capture"] != true {
		t.Errorf("expected capture=true")
	}
	receipt, ok := payload["receipt"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected NPD receipt map in payload")
	}
	items := receipt["items"].([]map[string]interface{})
	if items[0]["vat_code"] != 1 || items[0]["payment_subject"] != "service" {
		t.Errorf("expected NPD vat_code=1 and payment_subject=service, got %+v", items[0])
	}
}

func TestIsAllowedWebhookIP(t *testing.T) {
	svc := NewService(config.Config{YooVerifyIP: true}, nil, nil, nil, nil)

	hAllowed := http.Header{}
	hAllowed.Set("X-Real-IP", "185.71.76.10")
	if !svc.IsAllowedWebhookIP("172.18.0.5:1234", hAllowed) {
		t.Errorf("expected official YooKassa CIDR IP to be allowed")
	}

	hBlocked := http.Header{}
	hBlocked.Set("X-Real-IP", "8.8.8.8")
	if svc.IsAllowedWebhookIP("172.18.0.5:1234", hBlocked) {
		t.Errorf("expected external IP 8.8.8.8 to be blocked when YooVerifyIP=true")
	}
}
