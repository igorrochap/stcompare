package comparison

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	rules := responseNormalizationRules(config)
	normalized := NormalizedResponse{
		Status:  response.Status,
		Headers: append([]harHeader(nil), response.Headers...),
		Disclosure: NormalizationDisclosure{
			Rules: make([]NormalizationRuleDisclosure, 0, len(rules)),
		},
	}
	if response.Body != nil {
		body := *response.Body
		normalized.Body = &body
	}

	matchesByRule := make([][]string, len(rules))
	if normalized.Body == nil {
		normalized.Disclosure.Body.State = BodyNormalizationStateAbsent
	} else if *normalized.Body == "" {
		normalized.Disclosure.Body.State = BodyNormalizationStateEmpty
	} else {
		var body any
		if err := json.Unmarshal([]byte(*normalized.Body), &body); err != nil {
			normalized.Disclosure.Body.State = BodyNormalizationStateUnparseable
		} else {
			normalized.Disclosure.Body.State = BodyNormalizationStateJSON
			for index, rule := range rules {
				if rule.target != "body" {
					continue
				}
				matchesByRule[index] = normalizeJSONField(
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
				return NormalizedResponse{}, fmt.Errorf("encode normalized response body: %w", err)
			}
			bodyText := strings.TrimSuffix(contents.String(), "\n")
			normalized.Body = &bodyText
		}
	}

	for index, rule := range rules {
		if rule.target != "header" {
			continue
		}
		for headerIndex := range normalized.Headers {
			if !strings.EqualFold(normalized.Headers[headerIndex].Name, rule.match) {
				continue
			}
			normalized.Headers[headerIndex].Value = rule.replacement
			matchesByRule[index] = append(
				matchesByRule[index],
				normalized.Headers[headerIndex].Name,
			)
		}
		sort.Strings(matchesByRule[index])
	}

	for index, rule := range rules {
		matches := matchesByRule[index]
		normalized.Disclosure.Rules = append(normalized.Disclosure.Rules, NormalizationRuleDisclosure{
			Name:        rule.name,
			Target:      rule.target,
			Selector:    rule.selector,
			Replacement: rule.replacement,
			Matched:     len(matches) > 0,
			Matches:     matches,
		})
	}

	return normalized, nil
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
