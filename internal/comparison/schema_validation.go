package comparison

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

const (
	openAPIValidatorName                 = "github.com/getkin/kin-openapi/openapi3filter"
	openAPIValidatorVersion              = "v0.145.0"
	openAPIValidatorSupportedOpenAPI     = "3.0, 3.1, 3.2"
	openAPIValidatorSupportedJSONSchemas = "OpenAPI 3.0 Schema Object; JSON Schema 2020-12 for OpenAPI 3.1+"
)

type OpenAPIContract struct {
	document    *openapi3.T
	limitation  problemOutcomeReason
	loadMessage string
}

type schemaValidationRequest struct {
	Method   string
	URL      string
	Response reportResponse
}

type schemaValidationResult struct {
	OutcomeReason problemOutcomeReason
	Errors        []string
}

type schemaValidationProvenance struct {
	Validator                      string               `json:"validator"`
	ValidatorVersion               string               `json:"validator_version"`
	SupportedOpenAPIVersions       string               `json:"supported_openapi_versions"`
	SupportedJSONSchemaDialects    string               `json:"supported_json_schema_dialects"`
	OpenAPIVersion                 string               `json:"openapi_version,omitempty"`
	JSONSchemaDialect              string               `json:"json_schema_dialect,omitempty"`
	ContractLimitation             problemOutcomeReason `json:"contract_limitation,omitempty"`
	ContractLimitationMessage      string               `json:"contract_limitation_message,omitempty"`
	ResponseValidationUsesRawBody  bool                 `json:"response_validation_uses_raw_body"`
	ResponseMediaTypeSource        string               `json:"response_media_type_source"`
	OperationResolutionTieBehavior string               `json:"operation_resolution_tie_behavior"`
}

func LoadOpenAPIContract(path string) *OpenAPIContract {
	if strings.TrimSpace(path) == "" {
		return &OpenAPIContract{
			limitation: problemOutcomeReasonSchemaContractUnavailable,
		}
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return &OpenAPIContract{
			limitation:  problemOutcomeReasonSchemaContractUnreadable,
			loadMessage: err.Error(),
		}
	}

	return loadOpenAPIContract(contents)
}

func loadOpenAPIContract(contents []byte) *OpenAPIContract {
	loader := openapi3.NewLoader()
	document, err := loader.LoadFromData(contents)
	if err != nil {
		return &OpenAPIContract{
			limitation:  problemOutcomeReasonSchemaReferenceUnresolved,
			loadMessage: err.Error(),
		}
	}
	if !supportedOpenAPIVersion(document) {
		return &OpenAPIContract{
			document:   document,
			limitation: problemOutcomeReasonSchemaOpenAPIVersionUnsupported,
		}
	}
	return &OpenAPIContract{document: document}
}

func supportedOpenAPIVersion(document *openapi3.T) bool {
	return document != nil &&
		(document.IsOpenAPI30() || document.IsOpenAPI31OrLater())
}

func (c *OpenAPIContract) Provenance() schemaValidationProvenance {
	provenance := schemaValidationProvenance{
		Validator:                      openAPIValidatorName,
		ValidatorVersion:               openAPIValidatorVersion,
		SupportedOpenAPIVersions:       openAPIValidatorSupportedOpenAPI,
		SupportedJSONSchemaDialects:    openAPIValidatorSupportedJSONSchemas,
		ResponseValidationUsesRawBody:  true,
		ResponseMediaTypeSource:        "candidate_response_headers",
		OperationResolutionTieBehavior: "literal_segments_then_parameter_count; equal-specificity matches are inconclusive",
	}
	if c == nil {
		provenance.ContractLimitation = problemOutcomeReasonSchemaContractUnavailable
		return provenance
	}
	provenance.ContractLimitation = c.limitation
	provenance.ContractLimitationMessage = c.loadMessage
	if c.document != nil {
		provenance.OpenAPIVersion = c.document.OpenAPI
		provenance.JSONSchemaDialect = c.document.JSONSchemaDialect
	}

	return provenance
}

func (c *OpenAPIContract) Validate(request schemaValidationRequest) schemaValidationResult {
	if c == nil {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaContractUnavailable,
		}
	}
	if c.limitation != "" {
		return schemaValidationResult{OutcomeReason: c.limitation}
	}

	route, reason := c.resolveOperation(request.Method, request.URL)
	if reason != "" {
		return schemaValidationResult{OutcomeReason: reason}
	}

	response, reason := c.resolveResponse(route, request.Response.Status)
	if reason != "" {
		return schemaValidationResult{OutcomeReason: reason}
	}
	mediaType, ok := responseMediaType(request.Response.Headers)
	if !ok {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaMediaTypeMissing,
		}
	}
	if contentType := response.Value.Content.Get(mediaType); contentType == nil {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaMediaTypeUnsupported,
		}
	} else if contentType.Schema == nil {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaResponseSchemaMissing,
		}
	}
	if request.Response.Body == "" {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaResponseBodyUnavailable,
		}
	}

	httpRequest, err := http.NewRequest(request.Method, request.URL, nil)
	if err != nil {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaOperationNotFound,
		}
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: httpRequest,
			Route:   route,
		},
		Status: request.Response.Status,
		Header: responseHeader(request.Response.Headers),
		Body:   io.NopCloser(strings.NewReader(request.Response.Body)),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			MultiError:            true,
		},
	}

	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		return classifyResponseValidationError(err)
	}

	return schemaValidationResult{
		OutcomeReason: problemOutcomeReasonSchemaValidationPassed,
	}
}

func (c *OpenAPIContract) resolveOperation(
	method string,
	rawURL string,
) (*routers.Route, problemOutcomeReason) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, problemOutcomeReasonSchemaOperationNotFound
	}
	requestSegments := splitPath(parsed.EscapedPath())
	normalizedMethod := strings.ToUpper(method)
	if c.document.Paths == nil {
		return nil, problemOutcomeReasonSchemaOperationNotFound
	}

	var matches []operationMatch
	for _, template := range c.document.Paths.Keys() {
		pathItem := c.document.Paths.Value(template)
		if pathItem == nil {
			continue
		}
		operation := pathItem.GetOperation(normalizedMethod)
		if operation == nil {
			continue
		}
		match, ok := matchPathTemplate(template, requestSegments)
		if ok {
			match.template = template
			match.pathItem = pathItem
			match.operation = operation
			matches = append(matches, match)
		}
	}
	if len(matches) == 0 {
		return nil, problemOutcomeReasonSchemaOperationNotFound
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].literalSegments != matches[j].literalSegments {
			return matches[i].literalSegments > matches[j].literalSegments
		}
		return matches[i].parameterSegments < matches[j].parameterSegments
	})
	best := matches[0]
	if len(matches) > 1 &&
		best.literalSegments == matches[1].literalSegments &&
		best.parameterSegments == matches[1].parameterSegments {
		return nil, problemOutcomeReasonSchemaOperationAmbiguous
	}

	return &routers.Route{
		Spec:      c.document,
		Path:      best.template,
		PathItem:  best.pathItem,
		Method:    normalizedMethod,
		Operation: best.operation,
	}, ""
}

type operationMatch struct {
	template          string
	pathItem          *openapi3.PathItem
	operation         *openapi3.Operation
	literalSegments   int
	parameterSegments int
}

func matchPathTemplate(template string, requestSegments []string) (operationMatch, bool) {
	templateSegments := splitPath(template)
	if len(templateSegments) != len(requestSegments) {
		return operationMatch{}, false
	}

	var match operationMatch
	for index, templateSegment := range templateSegments {
		if strings.HasPrefix(templateSegment, "{") &&
			strings.HasSuffix(templateSegment, "}") {
			if requestSegments[index] == "" {
				return operationMatch{}, false
			}
			match.parameterSegments++
			continue
		}
		if templateSegment != requestSegments[index] {
			return operationMatch{}, false
		}
		match.literalSegments++
	}

	return match, true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "/")
}

func (c *OpenAPIContract) resolveResponse(
	route *routers.Route,
	status int,
) (*openapi3.ResponseRef, problemOutcomeReason) {
	responses := route.Operation.Responses
	if responses == nil || responses.Len() == 0 {
		return nil, problemOutcomeReasonSchemaResponseSchemaMissing
	}
	response := responses.Status(status)
	if response == nil {
		response = responses.Default()
	}
	if response == nil || response.Value == nil {
		return nil, problemOutcomeReasonSchemaResponseSchemaMissing
	}

	return response, ""
}

func responseMediaType(headers []harHeader) (string, bool) {
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Content-Type") {
			continue
		}
		mediaType := strings.TrimSpace(strings.Split(header.Value, ";")[0])
		if mediaType == "" {
			return "", false
		}

		return strings.ToLower(mediaType), true
	}

	return "", false
}

func responseHeader(headers []harHeader) http.Header {
	responseHeader := make(http.Header, len(headers))
	for _, header := range headers {
		responseHeader.Add(header.Name, header.Value)
	}

	return responseHeader
}

func classifyResponseValidationError(err error) schemaValidationResult {
	var responseError *openapi3filter.ResponseError
	if !errors.As(err, &responseError) {
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaReferenceUnresolved,
			Errors:        []string{err.Error()},
		}
	}

	switch {
	case responseError.Reason == "failed to decode response body":
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaResponseBodyUnparseable,
		}
	case strings.HasPrefix(responseError.Reason, "response header Content-Type has unexpected value"):
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaMediaTypeUnsupported,
		}
	case responseError.Reason == "status is not supported":
		return schemaValidationResult{
			OutcomeReason: problemOutcomeReasonSchemaResponseSchemaMissing,
		}
	}

	return schemaValidationResult{
		OutcomeReason: problemOutcomeReasonRepeatedSchemaViolation,
		Errors:        schemaValidationErrors(responseError),
	}
}

func schemaValidationErrors(responseError *openapi3filter.ResponseError) []string {
	var multiError openapi3.MultiError
	if errors.As(responseError.Err, &multiError) {
		messages := make([]string, 0, len(multiError))
		for _, err := range multiError {
			messages = append(messages, schemaErrorMessage(err))
		}
		sort.Strings(messages)

		return messages
	}

	return []string{schemaErrorMessage(responseError)}
}

func schemaErrorMessage(err error) string {
	var schemaError *openapi3.SchemaError
	if errors.As(err, &schemaError) {
		pointer := strings.Join(schemaError.JSONPointer(), ".")
		if pointer == "" {
			pointer = "body"
		} else {
			pointer = "body." + pointer
		}

		return fmt.Sprintf("%s: %s", pointer, schemaError.Reason)
	}

	return err.Error()
}
