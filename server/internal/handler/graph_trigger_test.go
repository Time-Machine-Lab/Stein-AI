package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestGraphTriggersRequireHumanManagementAndBoundIngress(t *testing.T) {
	h := &Handler{GraphService: &service.GraphService{}, WebhookAbsoluteIPRateLimiter: NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})}
	for _, handler := range []http.HandlerFunc{h.ListGraphTriggers, h.SaveGraphTrigger, h.DeleteGraphTrigger, h.RotateGraphTriggerSecret, h.ListGraphTriggerEvents} {
		r := httptest.NewRequest(http.MethodPost, "/api/graph-triggers/test", nil)
		r.Header.Set("X-Actor-Source", "task_token")
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != 403 {
			t.Fatalf("machine management accepted: %d", w.Code)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		r := httptest.NewRequest(http.MethodPost, "/api/graph-triggers/test/invoke", nil)
		w := httptest.NewRecorder()
		h.InvokeGraphTrigger(w, r)
		expected := 403
		if attempt == 1 {
			expected = 429
		}
		if w.Code != expected {
			t.Fatalf("attempt%d got%d want%d", attempt, w.Code, expected)
		}
	}
}
