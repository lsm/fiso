package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	sigyaml "sigs.k8s.io/yaml"

	v1alpha1 "github.com/lsm/fiso/api/v1alpha1"
	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/link"
)

// RunExport converts flat flow and link configs to Kubernetes CRD format.
func RunExport(args []string, w io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso export [path] [--format=crd]

Converts flat flow and link configuration files to Kubernetes CRD manifests.

Options:
  --format=crd    Output as FlowDefinition and LinkTarget CRDs (default)
  --namespace=NS  Set metadata.namespace on exported resources (default: fiso-system)

Examples:
  fiso export                     Export from ./fiso
  fiso export ./my-project/fiso   Export from a custom path
  fiso export --namespace=prod    Export with custom namespace`)
		return nil
	}

	if w == nil {
		w = os.Stdout
	}

	dir := "./fiso"
	namespace := "fiso-system"
	format := "crd"

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--format="):
			format = strings.TrimPrefix(arg, "--format=")
		case strings.HasPrefix(arg, "--namespace="):
			namespace = strings.TrimPrefix(arg, "--namespace=")
		case !strings.HasPrefix(arg, "-"):
			dir = arg
		}
	}

	if format != "crd" {
		return fmt.Errorf("unsupported format %q (supported: crd)", format)
	}

	var docs [][]byte

	// Export flow definitions
	flowDir := filepath.Join(dir, "flows")
	if _, err := os.Stat(flowDir); err == nil {
		flowDocs, err := exportFlows(flowDir, namespace)
		if err != nil {
			return fmt.Errorf("export flows: %w", err)
		}
		docs = append(docs, flowDocs...)
	}

	// Export link targets
	linkPath := filepath.Join(dir, "link", "config.yaml")
	if _, err := os.Stat(linkPath); err == nil {
		linkDocs, err := exportLinks(linkPath, namespace)
		if err != nil {
			return fmt.Errorf("export links: %w", err)
		}
		docs = append(docs, linkDocs...)
	}

	if len(docs) == 0 {
		return fmt.Errorf("no flow or link configs found in %s", dir)
	}

	for i, doc := range docs {
		if i > 0 {
			if _, err := fmt.Fprintln(w, "---"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(w, string(doc)); err != nil {
			return err
		}
	}

	return nil
}

func exportFlows(dir, namespace string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var docs [][]byte
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var flow config.FlowDefinition
		if err := yaml.Unmarshal(data, &flow); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		crd := convertFlowToCRD(&flow, namespace)
		out, err := sigyaml.Marshal(crd)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", flow.Name, err)
		}
		docs = append(docs, out)
	}

	return docs, nil
}

func exportLinks(path, namespace string) ([][]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg link.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var docs [][]byte
	for i := range cfg.Targets {
		crd := convertLinkToCRD(&cfg.Targets[i], namespace)
		out, err := sigyaml.Marshal(crd)
		if err != nil {
			return nil, fmt.Errorf("marshal link %s: %w", cfg.Targets[i].Name, err)
		}
		docs = append(docs, out)
	}

	return docs, nil
}

func convertFlowToCRD(flow *config.FlowDefinition, namespace string) *v1alpha1.FlowDefinition {
	crd := &v1alpha1.FlowDefinition{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.Group + "/" + v1alpha1.Version,
			Kind:       "FlowDefinition",
		},
		ObjectMeta: v1alpha1.ObjectMeta{
			Name:      flow.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":    "fiso",
				"app.kubernetes.io/managed-by": "fiso-export",
			},
		},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{
				Type:   flow.Source.Type,
				Config: stringifyMap(flow.Source.Config),
			},
			Sink: v1alpha1.SinkSpec{
				Type:   flow.Sink.Type,
				Config: stringifyMap(flow.Sink.Config),
			},
			ErrorHandling: v1alpha1.ErrorHandlingSpec{
				DeadLetterTopic: flow.ErrorHandling.DeadLetterTopic,
				MaxRetries:      flow.ErrorHandling.MaxRetries,
			},
		},
	}

	if flow.Transform != nil && len(flow.Transform.Fields) > 0 {
		crd.Spec.Transform = &v1alpha1.TransformSpec{
			Fields: flow.Transform.Fields,
		}
	}

	// Preserve CloudEvents config as annotations
	if flow.CloudEvents != nil {
		if crd.Annotations == nil {
			crd.Annotations = make(map[string]string)
		}
		if flow.CloudEvents.Type != "" {
			crd.Annotations["fiso.io/cloudevents-type"] = flow.CloudEvents.Type
		}
		if flow.CloudEvents.Source != "" {
			crd.Annotations["fiso.io/cloudevents-source"] = flow.CloudEvents.Source
		}
		if flow.CloudEvents.Subject != "" {
			crd.Annotations["fiso.io/cloudevents-subject"] = flow.CloudEvents.Subject
		}
	}

	return crd
}

func convertLinkToCRD(target *link.LinkTarget, namespace string) *v1alpha1.LinkTarget {
	crd := &v1alpha1.LinkTarget{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.Group + "/" + v1alpha1.Version,
			Kind:       "LinkTarget",
		},
		ObjectMeta: v1alpha1.ObjectMeta{
			Name:      target.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":    "fiso",
				"app.kubernetes.io/managed-by": "fiso-export",
			},
		},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol:     target.Protocol,
			Host:         target.Host,
			AllowedPaths: target.AllowedPaths,
		},
	}

	// Auth mapping
	if target.Auth.Type != "" && target.Auth.Type != "none" {
		auth := &v1alpha1.LinkAuthSpec{
			Type: target.Auth.Type,
		}
		if target.Auth.SecretRef != nil {
			auth.SecretName = target.Auth.SecretRef.FilePath
			if auth.SecretName == "" {
				auth.SecretName = target.Auth.SecretRef.EnvVar
			}
		}
		if target.Auth.VaultRef != nil {
			auth.VaultPath = target.Auth.VaultRef.Path
		}
		crd.Spec.Auth = auth
	}

	// Circuit breaker (only if enabled)
	if target.CircuitBreaker.Enabled {
		crd.Spec.CircuitBreaker = &v1alpha1.CircuitBreakerSpec{
			FailureThreshold: target.CircuitBreaker.FailureThreshold,
			ResetTimeout:     target.CircuitBreaker.ResetTimeout,
		}
	}

	// Retry
	if target.Retry.MaxAttempts > 0 {
		crd.Spec.Retry = &v1alpha1.RetrySpec{
			MaxAttempts: target.Retry.MaxAttempts,
			Backoff:     target.Retry.Backoff,
		}
	}

	return crd
}

// stringifyMap converts map[string]interface{} to map[string]string.
// Complex values (slices, maps) are serialized as comma-separated strings
// for simple lists or as JSON-like representation.
func stringifyMap(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = val
		case []interface{}:
			parts := make([]string, len(val))
			for i, item := range val {
				parts[i] = fmt.Sprintf("%v", item)
			}
			result[k] = strings.Join(parts, ",")
		default:
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}
