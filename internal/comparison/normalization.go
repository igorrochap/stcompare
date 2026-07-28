package comparison

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	configpkg "stcompare/internal/config"
)

type RecordedResponse struct {
	Status  int
	Headers []harHeader
	Body    *string
}

type ResponseNormalizationConfig struct {
	Defaults   bool
	BodyFields []BodyFieldNormalizationRule
	Headers    []HeaderNormalizationRule
}

type BodyFieldNormalizationRule struct {
	Name      string
	FieldName string
}

type HeaderNormalizationRule struct {
	Name       string
	HeaderName string
}

func NormalizationConfigFrom(source configpkg.NormalizationConfig) ResponseNormalizationConfig {
	config := ResponseNormalizationConfig{
		Defaults:   source.DefaultRules,
		BodyFields: make([]BodyFieldNormalizationRule, 0, len(source.BodyFields)),
		Headers:    make([]HeaderNormalizationRule, 0, len(source.Headers)),
	}
	for _, rule := range source.BodyFields {
		config.BodyFields = append(config.BodyFields, BodyFieldNormalizationRule{
			Name:      rule.Name,
			FieldName: rule.FieldName,
		})
	}
	for _, rule := range source.Headers {
		config.Headers = append(config.Headers, HeaderNormalizationRule{
			Name:       rule.Name,
			HeaderName: rule.HeaderName,
		})
	}

	return config
}

type NormalizedResponse struct {
	Status     int
	Headers    []harHeader
	Body       *string
	Disclosure NormalizationDisclosure
}

type NormalizationDisclosure struct {
	Body  BodyNormalizationDisclosure
	Rules []NormalizationRuleDisclosure
}

type BodyNormalizationState string

const (
	BodyNormalizationStateAbsent      BodyNormalizationState = "absent"
	BodyNormalizationStateEmpty       BodyNormalizationState = "empty"
	BodyNormalizationStateJSON        BodyNormalizationState = "json"
	BodyNormalizationStateUnparseable BodyNormalizationState = "unparseable"
)

type BodyNormalizationDisclosure struct {
	State BodyNormalizationState
}

type NormalizationRuleDisclosure struct {
	Name        string
	Target      string
	Selector    string
	Replacement string
	Matched     bool
	Matches     []string
}

type compiledNormalizationRule struct {
	name        string
	target      string
	selector    string
	match       string
	replacement string
}

func NormalizeResponse(
	response RecordedResponse,
	config ResponseNormalizationConfig,
) (NormalizedResponse, error) {
	run := newNormalizationRun(responseNormalizationRules(config))
	normalized := NormalizedResponse{
		Status:  response.Status,
		Headers: append([]harHeader(nil), response.Headers...),
	}
	if response.Body != nil {
		body := *response.Body
		normalized.Body = &body
	}

	body, state, err := run.normalizeBody(normalized.Body)
	if err != nil {
		return NormalizedResponse{}, err
	}
	normalized.Body = body
	normalized.Disclosure.Body.State = state

	run.normalizeHeaders(normalized.Headers)
	normalized.Disclosure.Rules = run.disclosures()

	return normalized, nil
}

type normalizationRun struct {
	rules   []compiledNormalizationRule
	matches [][]string
}

func newNormalizationRun(rules []compiledNormalizationRule) *normalizationRun {
	return &normalizationRun{
		rules:   rules,
		matches: make([][]string, len(rules)),
	}
}

func (r *normalizationRun) normalizeBody(bodyText *string) (*string, BodyNormalizationState, error) {
	if bodyText == nil {
		return nil, BodyNormalizationStateAbsent, nil
	}
	if *bodyText == "" {
		return bodyText, BodyNormalizationStateEmpty, nil
	}

	var body any
	if err := json.Unmarshal([]byte(*bodyText), &body); err != nil {
		return bodyText, BodyNormalizationStateUnparseable, nil
	}
	for index, rule := range r.rules {
		if rule.target != "body" {
			continue
		}
		r.matches[index] = normalizeJSONField(
			body,
			rule.match,
			rule.replacement,
			"$",
		)
	}

	var contents bytes.Buffer
	encoder := json.NewEncoder(&contents)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return nil, "", fmt.Errorf("encode normalized response body: %w", err)
	}
	normalizedBody := strings.TrimSuffix(contents.String(), "\n")

	return &normalizedBody, BodyNormalizationStateJSON, nil
}

func (r *normalizationRun) normalizeHeaders(headers []harHeader) {
	for index, rule := range r.rules {
		if rule.target != "header" {
			continue
		}
		for headerIndex := range headers {
			if !strings.EqualFold(headers[headerIndex].Name, rule.match) {
				continue
			}
			headers[headerIndex].Value = rule.replacement
			r.matches[index] = append(r.matches[index], headers[headerIndex].Name)
		}
		sort.Strings(r.matches[index])
	}
}

func (r *normalizationRun) disclosures() []NormalizationRuleDisclosure {
	disclosures := make([]NormalizationRuleDisclosure, 0, len(r.rules))
	for index, rule := range r.rules {
		matches := r.matches[index]
		disclosures = append(disclosures, NormalizationRuleDisclosure{
			Name:        rule.name,
			Target:      rule.target,
			Selector:    rule.selector,
			Replacement: rule.replacement,
			Matched:     len(matches) > 0,
			Matches:     matches,
		})
	}

	return disclosures
}

func responseNormalizationRules(config ResponseNormalizationConfig) []compiledNormalizationRule {
	var rules []compiledNormalizationRule
	if config.Defaults {
		rules = append(rules,
			bodyFieldRule("generated-id", "id"),
			bodyFieldRule("generated-uuid", "uuid"),
			bodyFieldRule("timestamp", "created_at"),
			bodyFieldRule("timestamp", "updated_at"),
			bodyFieldRule("timestamp", "timestamp"),
			headerRule("date-header", "date"),
		)
	}
	for _, rule := range config.BodyFields {
		rules = append(rules, bodyFieldRule(
			strings.TrimSpace(rule.Name),
			strings.TrimSpace(rule.FieldName),
		))
	}
	for _, rule := range config.Headers {
		rules = append(rules, headerRule(
			strings.TrimSpace(rule.Name),
			strings.TrimSpace(rule.HeaderName),
		))
	}

	return rules
}

func bodyFieldRule(name string, fieldName string) compiledNormalizationRule {
	return compiledNormalizationRule{
		name:        name,
		target:      "body",
		selector:    "field:" + fieldName,
		match:       fieldName,
		replacement: "<normalized:" + name + ">",
	}
}

func headerRule(name string, headerName string) compiledNormalizationRule {
	return compiledNormalizationRule{
		name:        name,
		target:      "header",
		selector:    "header:" + strings.ToLower(headerName),
		match:       headerName,
		replacement: "<normalized:" + name + ">",
	}
}

func normalizeJSONField(
	value any,
	fieldName string,
	replacement string,
	path string,
) []string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		var matches []string
		for _, key := range keys {
			fieldPath := path + "." + key
			if key == fieldName {
				typed[key] = replacement
				matches = append(matches, fieldPath)
				continue
			}
			matches = append(
				matches,
				normalizeJSONField(typed[key], fieldName, replacement, fieldPath)...,
			)
		}

		return matches
	case []any:
		var matches []string
		for index, item := range typed {
			matches = append(
				matches,
				normalizeJSONField(
					item,
					fieldName,
					replacement,
					fmt.Sprintf("%s[%d]", path, index),
				)...,
			)
		}

		return matches
	default:
		return nil
	}
}
