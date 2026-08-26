package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
	sigyaml "sigs.k8s.io/yaml"

	v1alpha1 "github.com/lsm/fiso/api/v1alpha1"
	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/link"
	unifiedxform "github.com/lsm/fiso/internal/transform/unified"
)

// RunExport converts flat flow and link configs to Kubernetes CRD format.
func RunExport(args []string, w io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		if w == nil {
			w = os.Stdout
		}
		_, err := fmt.Fprintln(w, `Usage: fiso export [path] [--format=crd]

Converts the losslessly representable subset of flat flow and link
configuration files to Kubernetes CRD manifests. Unsupported populated fields
fail with their resource path before any YAML is written.

Options:
  --format=crd    Output as FlowDefinition and LinkTarget CRDs (default)
  --namespace=NS  Set metadata.namespace on exported resources (default: fiso-system)

Examples:
  fiso export                     Export from ./fiso
  fiso export ./my-project/fiso   Export from a custom path
  fiso export --namespace=prod    Export with custom namespace`)
		return err
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
	if problems := validation.IsDNS1123Label(namespace); len(problems) > 0 {
		return unsupportedExportField("namespace", "not a valid Kubernetes namespace: "+strings.Join(problems, "; "))
	}

	var docs [][]byte

	// Export flow definitions
	flowDir := filepath.Join(dir, "flows")
	if _, err := os.Stat(flowDir); err == nil {
		flowDocs, exportErr := exportFlows(flowDir, namespace)
		if exportErr != nil {
			return fmt.Errorf("export flows: %w", exportErr)
		}
		docs = append(docs, flowDocs...)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect flow directory %s: %w", flowDir, err)
	}

	// Export link targets
	linkPath, err := findLinkConfig(dir)
	if err != nil {
		return err
	}
	if linkPath != "" {
		linkDocs, exportErr := exportLinks(linkPath, namespace)
		if exportErr != nil {
			return fmt.Errorf("export links: %w", exportErr)
		}
		docs = append(docs, linkDocs...)
	}

	if len(docs) == 0 {
		return fmt.Errorf("no flow or link configs found in %s", dir)
	}

	var output bytes.Buffer
	for i, doc := range docs {
		if i > 0 {
			output.WriteString("---\n")
		}
		output.Write(doc)
	}
	_, err = w.Write(output.Bytes())
	return err
}

func findLinkConfig(dir string) (string, error) {
	candidates := []string{
		filepath.Join(dir, "link", "config.yaml"),
		filepath.Join(dir, "link-config.yaml"),
		filepath.Join(dir, "links.yaml"),
	}
	var found []string
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect link config %s: %w", path, err)
		}
	}
	if len(found) > 1 {
		return "", fmt.Errorf("multiple link configs found: %s", strings.Join(found, ", "))
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return "", nil
}

func exportFlows(dir, namespace string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var docs [][]byte
	names := make(map[string]string)
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
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateFlowFieldTypes(&root); err != nil {
			return nil, fmt.Errorf("validate %s: %w", path, err)
		}
		if err := decodeKnownFields(data, &flow); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := flow.Validate(); err != nil {
			return nil, fmt.Errorf("validate %s: %w", path, err)
		}
		if flow.Transform != nil && len(flow.Transform.Fields) > 0 {
			if _, err := unifiedxform.NewTransformer(flow.Transform.Fields); err != nil {
				return nil, fmt.Errorf("validate %s transform.fields: %w", path, err)
			}
		}
		if err := validateExportableFlow(&flow); err != nil {
			return nil, fmt.Errorf("flow %q: %w", flow.Name, err)
		}
		if previous, ok := names[flow.Name]; ok {
			return nil, fmt.Errorf("duplicate FlowDefinition name %q in %s and %s", flow.Name, previous, path)
		}
		names[flow.Name] = path

		crd, err := convertFlowToCRD(&flow, namespace)
		if err != nil {
			return nil, fmt.Errorf("convert %s: %w", flow.Name, err)
		}
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
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validateLinkFieldTypes(&root); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	if err := decodeKnownFields(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range cfg.Targets {
		if cfg.Targets[i].Protocol == "" {
			cfg.Targets[i].Protocol = "https"
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	if err := validateExportableLink(&cfg); err != nil {
		return nil, fmt.Errorf("link config: %w", err)
	}

	var docs [][]byte
	names := make(map[string]int)
	for i := range cfg.Targets {
		if previous, ok := names[cfg.Targets[i].Name]; ok {
			return nil, fmt.Errorf("duplicate LinkTarget name %q at targets[%d] and targets[%d]", cfg.Targets[i].Name, previous, i)
		}
		names[cfg.Targets[i].Name] = i
		crd := convertLinkToCRD(&cfg.Targets[i], namespace)
		out, err := sigyaml.Marshal(crd)
		if err != nil {
			return nil, fmt.Errorf("marshal link %s: %w", cfg.Targets[i].Name, err)
		}
		docs = append(docs, out)
	}

	return docs, nil
}

func decodeKnownFields(data []byte, target interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple YAML documents are not supported")
	}
	return nil
}

func validateExportableFlow(flow *config.FlowDefinition) error {
	if problems := validation.IsDNS1123Subdomain(flow.Name); len(problems) > 0 {
		return unsupportedExportField("name", "not a valid Kubernetes metadata.name: "+strings.Join(problems, "; "))
	}
	if flow.Source.Type == "http" {
		return unsupportedExportField("source.type", "http is not supported by fiso.io/v1alpha1 FlowDefinition")
	}
	if len(flow.Kafka.Clusters) > 0 {
		return unsupportedExportField("kafka", "named Kafka clusters have no FlowDefinition representation")
	}
	if flow.CloudEvents != nil {
		return unsupportedExportField("cloudevents", "CloudEvents overrides have no executable FlowDefinition representation")
	}
	if len(flow.Interceptors) > 0 {
		return unsupportedExportField("interceptors", "interceptors have no FlowDefinition representation")
	}
	if _, err := copyStringMap("source.config", flow.Source.Config); err != nil {
		return err
	}
	if _, err := copyStringMap("sink.config", flow.Sink.Config); err != nil {
		return err
	}
	return nil
}

func validateFlowFieldTypes(root *yaml.Node) error {
	document := yamlDocument(root)
	if err := rejectYAMLMergeKeys(document, ""); err != nil {
		return err
	}
	if err := requireYAMLScalarTag(yamlMappingValue(document, "name"), "name", "!!str"); err != nil {
		return err
	}
	if err := requireYAMLScalarTag(yamlMappingValue(yamlMappingValue(document, "source"), "type"), "source.type", "!!str"); err != nil {
		return err
	}
	if err := requireYAMLScalarTag(yamlMappingValue(yamlMappingValue(document, "sink"), "type"), "sink.type", "!!str"); err != nil {
		return err
	}
	if err := validateStringMap(yamlMappingValue(yamlMappingValue(document, "source"), "config"), "source.config"); err != nil {
		return err
	}
	if err := validateStringMap(yamlMappingValue(yamlMappingValue(document, "sink"), "config"), "sink.config"); err != nil {
		return err
	}
	if err := validateStringMap(yamlMappingValue(yamlMappingValue(document, "transform"), "fields"), "transform.fields"); err != nil {
		return err
	}
	errorHandling := yamlMappingValue(document, "errorHandling")
	if err := validateYAMLScalarTags(errorHandling, "errorHandling", map[string][]string{
		"deadLetterTopic": {"!!str"},
		"maxRetries":      {"!!int"},
		"backoff":         {"!!str"},
		"commitPolicy":    {"!!str"},
		"transactionalId": {"!!str"},
	}); err != nil {
		return err
	}
	if maxRetries := yamlMappingValue(errorHandling, "maxRetries"); maxRetries != nil {
		var retries int
		if err := maxRetries.Decode(&retries); err == nil && retries == 0 {
			return unsupportedExportField("errorHandling.maxRetries", "explicit zero would be omitted from the FlowDefinition")
		}
	}
	if deadLetterTopic := yamlMappingValue(errorHandling, "deadLetterTopic"); deadLetterTopic != nil && deadLetterTopic.Value == "" {
		return unsupportedExportField("errorHandling.deadLetterTopic", "explicit empty value would be omitted from the FlowDefinition")
	}
	for _, field := range []string{"backoff", "commitPolicy", "transactionalId"} {
		if yamlMappingValue(errorHandling, field) != nil {
			return unsupportedExportField("errorHandling."+field, field+" has no FlowDefinition representation")
		}
	}
	cloudEvents := yamlMappingValue(document, "cloudevents")
	if cloudEvents != nil && cloudEvents.Kind == yaml.MappingNode {
		for _, field := range []string{"id", "type", "source", "subject", "data", "datacontenttype", "dataschema"} {
			if err := requireYAMLScalarTag(yamlMappingValue(cloudEvents, field), "cloudevents."+field, "!!str"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStringMap(mapping *yaml.Node, prefix string) error {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		path := prefix + "." + mapping.Content[i].Value
		if err := requireYAMLScalarTag(mapping.Content[i], path, "!!str"); err != nil {
			return err
		}
		if err := requireYAMLScalarTag(mapping.Content[i+1], path, "!!str"); err != nil {
			return err
		}
	}
	return nil
}

func rejectYAMLMergeKeys(node *yaml.Node, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return unsupportedExportField(path, "YAML aliases are not supported")
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			fieldName := key.Value
			if key.Tag != "!!str" && key.Tag != "!!merge" {
				var decoded string
				if err := key.Decode(&decoded); err == nil {
					fieldName = decoded
				}
			}
			fieldPath := fieldName
			if path != "" {
				fieldPath = path + "." + fieldName
			}
			if key.Tag == "!!merge" {
				return unsupportedExportField(fieldPath, "YAML merge keys are not supported")
			}
			if key.Tag != "!!str" {
				return unsupportedExportField(fieldPath, fmt.Sprintf("field name has YAML type %s", key.Tag))
			}
			if err := rejectYAMLMergeKeys(node.Content[i+1], fieldPath); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		for i, value := range node.Content {
			valuePath := fmt.Sprintf("%s[%d]", path, i)
			if err := rejectYAMLMergeKeys(value, valuePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireYAMLScalarTag(value *yaml.Node, path string, tags ...string) error {
	if value == nil {
		return nil
	}
	if value.Kind != yaml.ScalarNode {
		return unsupportedExportField(path, "unexpected YAML value type")
	}
	for _, tag := range tags {
		if value.Tag == tag {
			return nil
		}
	}
	return unsupportedExportField(path, fmt.Sprintf("value has YAML type %s", value.Tag))
}

func yamlDocument(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		return root.Content[0]
	}
	return root
}

func validateLinkFieldTypes(root *yaml.Node) error {
	document := yamlDocument(root)
	if err := rejectYAMLMergeKeys(document, ""); err != nil {
		return err
	}
	for _, field := range []string{"listenAddr", "metricsAddr"} {
		if yamlMappingValue(document, field) != nil {
			return unsupportedExportField(field, "process-level setting has no LinkTarget representation")
		}
	}
	targets := yamlMappingValue(document, "targets")
	if targets == nil || targets.Kind != yaml.SequenceNode {
		return nil
	}
	for i, target := range targets.Content {
		if target.Kind != yaml.MappingNode {
			continue
		}
		prefix := fmt.Sprintf("targets[%d]", i)
		for _, field := range []string{"name", "protocol", "host", "basePath"} {
			if err := requireYAMLScalarTag(yamlMappingValue(target, field), prefix+"."+field, "!!str"); err != nil {
				return err
			}
		}
		if protocol := yamlMappingValue(target, "protocol"); protocol != nil && protocol.Value == "" {
			return unsupportedExportField(prefix+".protocol", "explicit empty value cannot be replaced by the default protocol")
		}
		if yamlMappingValue(target, "basePath") != nil {
			return unsupportedExportField(prefix+".basePath", "base path has no LinkTarget representation")
		}
		if port := yamlMappingValue(target, "port"); port != nil {
			if err := requireYAMLScalarTag(port, prefix+".port", "!!int"); err != nil {
				return err
			}
			return unsupportedExportField(prefix+".port", "port has no LinkTarget representation")
		}
		auth := yamlMappingValue(target, "auth")
		if err := requireYAMLScalarTag(yamlMappingValue(auth, "type"), prefix+".auth.type", "!!str"); err != nil {
			return err
		}
		allowedPaths := yamlMappingValue(target, "allowedPaths")
		if allowedPaths != nil {
			if allowedPaths.Kind != yaml.SequenceNode {
				return unsupportedExportField(prefix+".allowedPaths", "unexpected YAML value type")
			}
			for j, value := range allowedPaths.Content {
				if err := requireYAMLScalarTag(value, fmt.Sprintf("%s.allowedPaths[%d]", prefix, j), "!!str"); err != nil {
					return err
				}
			}
		}
		circuitBreaker := yamlMappingValue(target, "circuitBreaker")
		if err := validateYAMLScalarTags(circuitBreaker, prefix+".circuitBreaker", map[string][]string{
			"enabled":          {"!!bool"},
			"failureThreshold": {"!!int"},
			"successThreshold": {"!!int"},
			"resetTimeout":     {"!!str"},
		}); err != nil {
			return err
		}
		if enabled := yamlMappingValue(circuitBreaker, "enabled"); enabled != nil {
			var value bool
			if err := enabled.Decode(&value); err == nil && !value {
				return unsupportedExportField(prefix+".circuitBreaker.enabled", "an explicitly disabled circuit breaker has no LinkTarget representation")
			}
		} else {
			for _, field := range []string{"failureThreshold", "resetTimeout"} {
				if yamlMappingValue(circuitBreaker, field) != nil {
					return unsupportedExportField(prefix+".circuitBreaker."+field, field+" without an enabled circuit breaker would be discarded")
				}
			}
		}
		if resetTimeout := yamlMappingValue(circuitBreaker, "resetTimeout"); resetTimeout != nil && resetTimeout.Value == "" {
			return unsupportedExportField(prefix+".circuitBreaker.resetTimeout", "explicit empty value would be omitted")
		}
		if yamlMappingValue(circuitBreaker, "successThreshold") != nil {
			return unsupportedExportField(prefix+".circuitBreaker.successThreshold", "success threshold has no LinkTarget representation")
		}
		if err := rejectUnknownYAMLFields(circuitBreaker, prefix+".circuitBreaker", map[string]bool{
			"enabled": true, "failureThreshold": true, "successThreshold": true, "resetTimeout": true,
		}); err != nil {
			return err
		}
		retry := yamlMappingValue(target, "retry")
		if err := validateYAMLScalarTags(retry, prefix+".retry", map[string][]string{
			"maxAttempts":     {"!!int"},
			"backoff":         {"!!str"},
			"initialInterval": {"!!str", "!!int"},
			"maxInterval":     {"!!str", "!!int"},
			"jitter":          {"!!int", "!!float"},
		}); err != nil {
			return err
		}
		if maxAttempts := yamlMappingValue(retry, "maxAttempts"); maxAttempts != nil {
			var attempts int
			if err := maxAttempts.Decode(&attempts); err == nil && attempts == 0 {
				return unsupportedExportField(prefix+".retry.maxAttempts", "explicit zero would be omitted and is invalid in the LinkTarget CRD")
			}
		}
		if backoff := yamlMappingValue(retry, "backoff"); backoff != nil && backoff.Value == "" {
			return unsupportedExportField(prefix+".retry.backoff", "explicit empty value would be omitted from the LinkTarget")
		}
		for _, field := range []string{"initialInterval", "maxInterval", "jitter"} {
			if yamlMappingValue(retry, field) != nil {
				return unsupportedExportField(prefix+".retry."+field, field+" has no LinkTarget representation")
			}
		}
		if err := rejectUnknownYAMLFields(yamlMappingValue(target, "retry"), prefix+".retry", map[string]bool{
			"maxAttempts": true, "backoff": true, "initialInterval": true, "maxInterval": true, "jitter": true,
		}); err != nil {
			return err
		}
		rateLimit := yamlMappingValue(target, "rateLimit")
		if err := validateYAMLScalarTags(rateLimit, prefix+".rateLimit", map[string][]string{
			"requestsPerSecond": {"!!int", "!!float"},
			"burst":             {"!!int"},
		}); err != nil {
			return err
		}
		if yamlMappingValue(rateLimit, "requestsPerSecond") != nil || yamlMappingValue(rateLimit, "burst") != nil {
			return unsupportedExportField(prefix+".rateLimit", "rate limiting has no LinkTarget representation")
		}
		if err := rejectUnknownYAMLFields(rateLimit, prefix+".rateLimit", map[string]bool{
			"requestsPerSecond": true,
			"burst":             true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func validateYAMLScalarTags(mapping *yaml.Node, prefix string, fields map[string][]string) error {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for field, tags := range fields {
		value := yamlMappingValue(mapping, field)
		path := prefix + "." + field
		if err := requireYAMLScalarTag(value, path, tags...); err != nil {
			return err
		}
		if value != nil && value.Tag == "!!int" {
			var integer int
			if err := value.Decode(&integer); err != nil {
				return unsupportedExportField(path, "integer is outside the supported range")
			}
		}
	}
	return nil
}

func rejectUnknownYAMLFields(mapping *yaml.Node, prefix string, allowed map[string]bool) error {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		field := mapping.Content[i]
		path := prefix + "." + field.Value
		if err := requireYAMLScalarTag(field, path, "!!str"); err != nil {
			return err
		}
		if !allowed[field.Value] {
			return unsupportedExportField(path, "unknown field")
		}
	}
	return nil
}

func yamlMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// validateExportableLink checks decoded Link settings that raw-node
// validation cannot see, such as Kubernetes name rules and block semantics.
// Field presence, scalar tags, and unknown keys are rejected earlier from the
// YAML node itself in validateLinkFieldTypes.
func validateExportableLink(cfg *link.Config) error {
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		prefix := fmt.Sprintf("targets[%d]", i)
		if problems := validation.IsDNS1123Subdomain(target.Name); len(problems) > 0 {
			return unsupportedExportField(prefix+".name", "not a valid Kubernetes metadata.name: "+strings.Join(problems, "; "))
		}
		if target.Protocol == "kafka" {
			return unsupportedExportField(prefix+".protocol", "Kafka targets have no LinkTarget representation")
		}
		if target.Auth.Type != "" && target.Auth.Type != "none" {
			return unsupportedExportField(prefix+".auth.type", "local authentication configuration has no lossless LinkTarget representation")
		}
		if target.Auth.SecretRef != nil {
			return unsupportedExportField(prefix+".auth.secretRef", "local file and environment references are not Kubernetes Secret names")
		}
		if target.Auth.VaultRef != nil {
			return unsupportedExportField(prefix+".auth.vaultRef", "local Vault settings have no lossless LinkTarget representation")
		}
		if target.CircuitBreaker.Enabled && target.CircuitBreaker.FailureThreshold < 1 {
			return unsupportedExportField(prefix+".circuitBreaker.failureThreshold", "enabled circuit breakers require a value of at least 1 in the LinkTarget CRD")
		}
		if target.Retry.MaxAttempts < 0 {
			return unsupportedExportField(prefix+".retry.maxAttempts", "negative values are rejected by the LinkTarget CRD")
		}
		if target.Retry.MaxAttempts == 0 && target.Retry.Backoff != "" {
			return unsupportedExportField(prefix+".retry.backoff", "backoff without enabled retries would be discarded")
		}
		if target.Kafka != nil {
			return unsupportedExportField(prefix+".kafka", "Kafka target settings have no LinkTarget representation")
		}
		if len(target.Interceptors) > 0 {
			return unsupportedExportField(prefix+".interceptors", "interceptors have no LinkTarget representation")
		}
	}
	if len(cfg.Kafka.Clusters) > 0 {
		return unsupportedExportField("kafka", "named Kafka clusters have no LinkTarget representation")
	}
	return nil
}

func unsupportedExportField(path, reason string) error {
	return fmt.Errorf("%s cannot be represented by fiso.io/v1alpha1: %s", path, reason)
}

func copyStringMap(path string, values map[string]interface{}) (map[string]string, error) {
	if values == nil {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, unsupportedExportField(path+"."+key, fmt.Sprintf("value has type %T; only strings are supported", value))
		}
		result[key] = text
	}
	return result, nil
}

func convertFlowToCRD(flow *config.FlowDefinition, namespace string) (*v1alpha1.FlowDefinition, error) {
	sourceConfig, err := copyStringMap("source.config", flow.Source.Config)
	if err != nil {
		return nil, err
	}
	sinkConfig, err := copyStringMap("sink.config", flow.Sink.Config)
	if err != nil {
		return nil, err
	}

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
				Config: sourceConfig,
			},
			Sink: v1alpha1.SinkSpec{
				Type:   flow.Sink.Type,
				Config: sinkConfig,
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

	return crd, nil
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
