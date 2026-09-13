package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventOmitsUnmeasuredDuration(t *testing.T) {
	data, err := json.Marshal(Event{Type: "model_turn", Status: "started"})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(data), `"duration_ms"`) {
		t.Fatalf("event = %s, want duration omitted when it is unmeasured", data)
	}
}

func TestReadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"2"}`), 0o644); err != nil {
		t.Fatalf("write audit fixture: %v", err)
	}
	if _, err := Read(path); err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("Read() error = %v, want schema version error", err)
	}
}
