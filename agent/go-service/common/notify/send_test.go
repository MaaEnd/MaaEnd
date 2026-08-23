package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSendDoesNotMutateVars 验证 Send 不隐式修改调用方传入的 vars map
// （内部会写回解析后的 title/body，但不应污染调用方数据）。
func TestSendDoesNotMutateVars(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{WebhookEnabled: true, WebhookURL: server.URL, WebhookMethod: "GET"}
	vars := map[string]string{"task_name": "T"}
	if !Send(config, vars) {
		t.Fatalf("Send returned false")
	}
	if _, ok := vars["title"]; ok {
		t.Errorf("Send mutated caller vars, added title: %v", vars)
	}
	if _, ok := vars["body"]; ok {
		t.Errorf("Send mutated caller vars, added body: %v", vars)
	}
	if vars["task_name"] != "T" {
		t.Errorf("Send mutated caller vars, task_name changed: %v", vars)
	}
}
