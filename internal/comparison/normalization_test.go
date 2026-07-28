package comparison

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeResponseMasksConfiguredJSONFieldsAndHeaders(t *testing.T) {
	body := `{"id":"baseline-123","name":"Ada","nested":{"request_id":"abc"},"items":[{"created_at":"2026-07-28T12:00:00Z"}]}`
	input := RecordedResponse{
		Status: 200,
		Headers: []harHeader{
			{Name: "date", Value: "Tue, 28 Jul 2026 12:00:00 GMT"},
			{Name: "content-type", Value: "application/json"},
			{Name: "Server", Value: "baseline-server"},
		},
		Body: &body,
	}

	got, err := NormalizeResponse(input, ResponseNormalizationConfig{
		Defaults: true,
		BodyFields: []BodyFieldNormalizationRule{
			{Name: "request-id", FieldName: "request_id"},
		},
		Headers: []HeaderNormalizationRule{
			{Name: "server-version", HeaderName: "server"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeResponse returned error: %v", err)
	}

	if !strings.Contains(*got.Body, `"id":"<normalized:generated-id>"`) {
		t.Fatalf("normalized body = %s, want default id mask", *got.Body)
	}
	if !strings.Contains(*got.Body, `"created_at":"<normalized:timestamp>"`) {
		t.Fatalf("normalized body = %s, want default timestamp mask", *got.Body)
	}
	if !strings.Contains(*got.Body, `"request_id":"<normalized:request-id>"`) {
		t.Fatalf("normalized body = %s, want configured request_id mask", *got.Body)
	}
	if !strings.Contains(*got.Body, `"name":"Ada"`) {
		t.Fatalf("normalized body = %s, want unrelated field preserved", *got.Body)
	}

	wantHeaders := []harHeader{
		{Name: "date", Value: "<normalized:date-header>"},
		{Name: "content-type", Value: "application/json"},
		{Name: "Server", Value: "<normalized:server-version>"},
	}
	if !reflect.DeepEqual(got.Headers, wantHeaders) {
		t.Fatalf("normalized headers = %#v, want %#v", got.Headers, wantHeaders)
	}

	wantRules := []NormalizationRuleDisclosure{
		{
			Name:        "generated-id",
			Target:      "body",
			Selector:    "field:id",
			Replacement: "<normalized:generated-id>",
			Matched:     true,
			Matches:     []string{"$.id"},
		},
		{
			Name:        "generated-uuid",
			Target:      "body",
			Selector:    "field:uuid",
			Replacement: "<normalized:generated-uuid>",
		},
		{
			Name:        "timestamp",
			Target:      "body",
			Selector:    "field:created_at",
			Replacement: "<normalized:timestamp>",
			Matched:     true,
			Matches:     []string{"$.items[0].created_at"},
		},
		{
			Name:        "timestamp",
			Target:      "body",
			Selector:    "field:updated_at",
			Replacement: "<normalized:timestamp>",
		},
		{
			Name:        "timestamp",
			Target:      "body",
			Selector:    "field:timestamp",
			Replacement: "<normalized:timestamp>",
		},
		{
			Name:        "date-header",
			Target:      "header",
			Selector:    "header:date",
			Replacement: "<normalized:date-header>",
			Matched:     true,
			Matches:     []string{"date"},
		},
		{
			Name:        "request-id",
			Target:      "body",
			Selector:    "field:request_id",
			Replacement: "<normalized:request-id>",
			Matched:     true,
			Matches:     []string{"$.nested.request_id"},
		},
		{
			Name:        "server-version",
			Target:      "header",
			Selector:    "header:server",
			Replacement: "<normalized:server-version>",
			Matched:     true,
			Matches:     []string{"Server"},
		},
	}
	if !reflect.DeepEqual(got.Disclosure.Rules, wantRules) {
		t.Fatalf("normalization rules = %#v, want %#v", got.Disclosure.Rules, wantRules)
	}
	if got.Disclosure.Body.State != BodyNormalizationStateJSON {
		t.Fatalf("body state = %q, want %q", got.Disclosure.Body.State, BodyNormalizationStateJSON)
	}
}

func TestNormalizeResponseDoesNotMutateRecordedResponse(t *testing.T) {
	body := `{"id":"raw-id"}`
	input := RecordedResponse{
		Headers: []harHeader{{Name: "date", Value: "raw-date"}},
		Body:    &body,
	}

	_, err := NormalizeResponse(input, ResponseNormalizationConfig{Defaults: true})
	if err != nil {
		t.Fatalf("NormalizeResponse returned error: %v", err)
	}

	if input.Headers[0].Value != "raw-date" {
		t.Fatalf("input header value = %q, want raw-date", input.Headers[0].Value)
	}
	if *input.Body != `{"id":"raw-id"}` {
		t.Fatalf("input body = %q, want raw body", *input.Body)
	}
}

func TestNormalizeResponseDisclosesUnaddressedBodyFormatsAndRulePresence(t *testing.T) {
	html := `<html><body>error 500</body></html>`
	got, err := NormalizeResponse(
		RecordedResponse{Body: &html},
		ResponseNormalizationConfig{BodyFields: []BodyFieldNormalizationRule{
			{Name: "request-id", FieldName: "request_id"},
		}},
	)
	if err != nil {
		t.Fatalf("NormalizeResponse returned error: %v", err)
	}

	if got.Body == nil || *got.Body != html {
		t.Fatalf("normalized body = %#v, want original HTML", got.Body)
	}
	if got.Disclosure.Body.State != BodyNormalizationStateUnparseable {
		t.Fatalf("body state = %q, want unparseable", got.Disclosure.Body.State)
	}
	if len(got.Disclosure.Rules) != 1 || got.Disclosure.Rules[0].Matched {
		t.Fatalf("rule disclosure = %#v, want one unmatched configured rule", got.Disclosure.Rules)
	}

	empty := ""
	gotEmpty, err := NormalizeResponse(RecordedResponse{Body: &empty}, ResponseNormalizationConfig{})
	if err != nil {
		t.Fatalf("NormalizeResponse empty body returned error: %v", err)
	}
	gotAbsent, err := NormalizeResponse(RecordedResponse{}, ResponseNormalizationConfig{})
	if err != nil {
		t.Fatalf("NormalizeResponse absent body returned error: %v", err)
	}
	if gotEmpty.Disclosure.Body.State != BodyNormalizationStateEmpty {
		t.Fatalf("empty body state = %q, want empty", gotEmpty.Disclosure.Body.State)
	}
	if gotAbsent.Disclosure.Body.State != BodyNormalizationStateAbsent {
		t.Fatalf("absent body state = %q, want absent", gotAbsent.Disclosure.Body.State)
	}
}
