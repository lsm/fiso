package delivery

import "testing"

func TestNormalizeCommitPolicy_Default(t *testing.T) {
	if got := NormalizeCommitPolicy(""); got != CommitPolicySinkOrDLQ {
		t.Fatalf("expected default %q, got %q", CommitPolicySinkOrDLQ, got)
	}
}

func TestNormalizeCommitPolicy_TrimAndLower(t *testing.T) {
	if got := NormalizeCommitPolicy("  SINK_OR_DLQ  "); got != CommitPolicySinkOrDLQ {
		t.Fatalf("expected normalized sink_or_dlq, got %q", got)
	}
}

func TestCommitPolicy_Valid(t *testing.T) {
	valid := []CommitPolicy{CommitPolicySink, CommitPolicySinkOrDLQ, CommitPolicyKafkaTransaction}
	for _, p := range valid {
		if !p.Valid() {
			t.Fatalf("expected policy %q to be valid", p)
		}
	}
	if CommitPolicy("bad").Valid() {
		t.Fatal("expected invalid policy")
	}
}
