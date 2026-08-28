package operator

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

type crdSchema struct {
	Spec struct {
		Versions []struct {
			Name   string `yaml:"name"`
			Schema struct {
				OpenAPIV3Schema struct {
					Properties struct {
						Spec struct {
							Properties struct {
								Protocol struct {
									Enum []string `yaml:"enum"`
								} `yaml:"protocol"`
							} `yaml:"properties"`
						} `yaml:"spec"`
					} `yaml:"properties"`
				} `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

// TestLinkTargetCRD_ProtocolEnumMatchesValidation pins that the LinkTarget CRD
// accepts exactly the protocols the shipped Link runtime can execute. grpc is
// rejected until a Link gRPC transport contract exists (issue #22).
func TestLinkTargetCRD_ProtocolEnumMatchesValidation(t *testing.T) {
	data, err := os.ReadFile("../../deploy/crds/fiso.io_linktargets.yaml")
	if err != nil {
		t.Fatalf("read LinkTarget CRD: %v", err)
	}

	var crd crdSchema
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("parse LinkTarget CRD: %v", err)
	}
	if len(crd.Spec.Versions) == 0 {
		t.Fatal("expected at least one CRD version")
	}

	for _, version := range crd.Spec.Versions {
		got := version.Schema.OpenAPIV3Schema.Properties.Spec.Properties.Protocol.Enum
		want := []string{"http", "https"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("version %s: protocol enum must be %v (only executable Link protocols), got %v", version.Name, want, got)
		}
	}
}
