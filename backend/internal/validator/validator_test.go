package validator

import (
	"strings"
	"testing"

	"otel-magnify/pkg/models"
)

const validMinimal = `
receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`

func TestValidate_Valid(t *testing.T) {
	r := Validate([]byte(validMinimal), nil)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %+v", r.Errors)
	}
}

func TestValidate_InvalidYAML(t *testing.T) {
	r := Validate([]byte("receivers: [oops\n"), nil)
	if r.Valid || len(r.Errors) == 0 || r.Errors[0].Code != "yaml_parse" {
		t.Fatalf("expected yaml_parse error, got %+v", r)
	}
}

func TestValidate_MissingService(t *testing.T) {
	r := Validate([]byte("receivers: {}\n"), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	if r.Errors[0].Code != "missing_service" {
		t.Errorf("first error = %+v, want missing_service", r.Errors[0])
	}
}

func TestValidate_UndefinedComponent(t *testing.T) {
	yaml := `
receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`
	r := Validate([]byte(yaml), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "undefined_component" && strings.Contains(e.Message, "batch") {
			found = true
		}
	}
	if !found {
		t.Errorf("undefined_component for 'batch' not reported, got %+v", r.Errors)
	}
}

func TestValidate_MissingPipelineSection(t *testing.T) {
	yaml := `
receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
`
	r := Validate([]byte(yaml), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "missing_pipeline_section" && strings.Contains(e.Message, "exporters") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing exporters not reported, got %+v", r.Errors)
	}
}

func TestValidate_ComponentNotInstalled(t *testing.T) {
	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers":  {"otlp"},
			"processors": {"batch"},
			"exporters":  {"logging"},
		},
	}
	yaml := `
receivers:
  jaeger: {}
processors:
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [jaeger]
      processors: [batch]
      exporters: [logging]
`
	r := Validate([]byte(yaml), available)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "component_not_installed" && strings.Contains(e.Message, "jaeger") {
			found = true
		}
	}
	if !found {
		t.Errorf("jaeger not-installed not reported, got %+v", r.Errors)
	}
}

func TestValidate_NamedInstanceStripsSuffix(t *testing.T) {
	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers": {"otlp"},
			"exporters": {"logging"},
		},
	}
	yaml := `
receivers:
  otlp/secondary: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp/secondary]
      exporters: [logging]
`
	r := Validate([]byte(yaml), available)
	if !r.Valid {
		t.Fatalf("named instance should resolve to base type, got errors: %+v", r.Errors)
	}
}

func TestValidate_SkipsCategoryNotReported(t *testing.T) {
	// If AvailableComponents only reports receivers, we must not flag an
	// unknown processor as not-installed — we just don't know.
	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers": {"otlp"},
			"exporters": {"logging"},
		},
	}
	r := Validate([]byte(validMinimal), available)
	if !r.Valid {
		t.Fatalf("expected valid when processor category not reported, got %+v", r.Errors)
	}
}
