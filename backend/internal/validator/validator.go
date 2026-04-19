// Package validator performs lightweight static validation of OTel Collector
// configurations before they are pushed to an agent.
//
// It parses the YAML, checks the mandatory "service.pipelines" shape, verifies
// that every pipeline component reference points to a definition in the matching
// top-level section, and (if provided) that each component's type is installed
// on the target agent according to its reported AvailableComponents.
//
// This is explicitly not a substitute for "otelcol validate": we don't resolve
// factories or check per-component option schemas. The goal is to catch the
// common mistakes — typos, missing definitions, components not built into the
// target collector — without running the collector binary.
package validator

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Error is a single validation failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Path is a dotted YAML path to the offending node, e.g. "service.pipelines.traces.receivers[0]".
	Path string `json:"path,omitempty"`
}

// Result is the outcome of a validation pass.
type Result struct {
	Valid  bool    `json:"valid"`
	Errors []Error `json:"errors,omitempty"`
}

// pipelineSectionToCategory maps the plural section name used inside a pipeline
// to the corresponding top-level section (both happen to match, but we keep
// the mapping explicit so callers don't rely on the identity).
var pipelineSectionToCategory = map[string]string{
	"receivers":  "receivers",
	"processors": "processors",
	"exporters":  "exporters",
}

// Validate runs the light validation. `available` may be nil, in which case
// only structural checks are performed. Returns a Result with Valid=true and
// no errors if everything checks out.
func Validate(yamlContent []byte, available *models.AvailableComponents) Result {
	var root map[string]any
	if err := yaml.Unmarshal(yamlContent, &root); err != nil {
		return Result{Errors: []Error{{
			Code:    "yaml_parse",
			Message: fmt.Sprintf("invalid YAML: %v", err),
		}}}
	}
	if root == nil {
		return Result{Errors: []Error{{
			Code:    "empty_config",
			Message: "configuration is empty",
		}}}
	}

	var errs []Error

	definedByCategory := map[string]map[string]bool{
		"receivers":  extractDefined(root, "receivers"),
		"processors": extractDefined(root, "processors"),
		"exporters":  extractDefined(root, "exporters"),
		"connectors": extractDefined(root, "connectors"),
		"extensions": extractDefined(root, "extensions"),
	}

	service, ok := root["service"].(map[string]any)
	if !ok {
		errs = append(errs, Error{
			Code: "missing_service", Message: "'service' section is required", Path: "service",
		})
		return Result{Errors: errs}
	}

	pipelines, ok := service["pipelines"].(map[string]any)
	if !ok || len(pipelines) == 0 {
		errs = append(errs, Error{
			Code: "missing_pipelines", Message: "'service.pipelines' must define at least one pipeline", Path: "service.pipelines",
		})
		return Result{Errors: errs}
	}

	// Sort pipeline names for deterministic error ordering.
	pipelineNames := make([]string, 0, len(pipelines))
	for name := range pipelines {
		pipelineNames = append(pipelineNames, name)
	}
	sort.Strings(pipelineNames)

	for _, name := range pipelineNames {
		pipelineRaw := pipelines[name]
		pipeline, ok := pipelineRaw.(map[string]any)
		if !ok {
			errs = append(errs, Error{
				Code: "invalid_pipeline", Message: fmt.Sprintf("pipeline %q is not an object", name),
				Path: "service.pipelines." + name,
			})
			continue
		}

		// A pipeline needs at least receivers and exporters.
		for _, required := range []string{"receivers", "exporters"} {
			if _, present := pipeline[required]; !present {
				errs = append(errs, Error{
					Code:    "missing_pipeline_section",
					Message: fmt.Sprintf("pipeline %q is missing '%s'", name, required),
					Path:    "service.pipelines." + name + "." + required,
				})
			}
		}

		for section, category := range pipelineSectionToCategory {
			refs := toStringSlice(pipeline[section])
			for i, id := range refs {
				path := fmt.Sprintf("service.pipelines.%s.%s[%d]", name, section, i)
				if _, defined := definedByCategory[category][id]; !defined {
					// A component referenced in a pipeline but not defined top-level is a hard error.
					errs = append(errs, Error{
						Code:    "undefined_component",
						Message: fmt.Sprintf("pipeline %q references %s %q which is not defined under top-level '%s'", name, singular(section), id, category),
						Path:    path,
					})
					continue
				}
				if available != nil {
					if msg := checkInstalled(category, id, available); msg != "" {
						errs = append(errs, Error{
							Code: "component_not_installed", Message: msg, Path: path,
						})
					}
				}
			}
		}
	}

	// Extensions declared in service.extensions must be defined too.
	if extRefs := toStringSlice(service["extensions"]); len(extRefs) > 0 {
		for i, id := range extRefs {
			path := fmt.Sprintf("service.extensions[%d]", i)
			if _, defined := definedByCategory["extensions"][id]; !defined {
				errs = append(errs, Error{
					Code: "undefined_component",
					Message: fmt.Sprintf("service.extensions references %q which is not defined under top-level 'extensions'", id),
					Path: path,
				})
				continue
			}
			if available != nil {
				if msg := checkInstalled("extensions", id, available); msg != "" {
					errs = append(errs, Error{Code: "component_not_installed", Message: msg, Path: path})
				}
			}
		}
	}

	return Result{Valid: len(errs) == 0, Errors: errs}
}

// extractDefined returns the set of component IDs defined under a top-level
// section (e.g. {"otlp": true, "otlp/secondary": true} for "receivers").
// Missing or malformed sections yield an empty set.
func extractDefined(root map[string]any, section string) map[string]bool {
	out := make(map[string]bool)
	m, ok := root[section].(map[string]any)
	if !ok {
		return out
	}
	for id := range m {
		out[id] = true
	}
	return out
}

// checkInstalled returns an empty string if the component type (everything
// before an optional "/instance_name" suffix) is listed in available
// components for the given category, otherwise a human-readable message.
// If the category is not reported at all, we assume the agent's view is
// incomplete and skip the check (stay conservative, not block push).
func checkInstalled(category, id string, available *models.AvailableComponents) string {
	installed, ok := available.Components[category]
	if !ok {
		return ""
	}
	componentType := id
	if idx := strings.Index(id, "/"); idx >= 0 {
		componentType = id[:idx]
	}
	if slices.Contains(installed, componentType) {
		return ""
	}
	return fmt.Sprintf("%s type %q is not installed on the target agent (available: %s)",
		singular(category), componentType, strings.Join(installed, ", "))
}

// toStringSlice coerces an arbitrary YAML value into a list of strings,
// silently dropping non-string entries. YAML lists decode as []any with
// element type string for identifiers like "otlp" or "batch/custom".
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func singular(section string) string {
	return strings.TrimSuffix(section, "s")
}
