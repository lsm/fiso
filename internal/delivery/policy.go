package delivery

import "strings"

// CommitPolicy controls when Kafka source offsets are acknowledged.
type CommitPolicy string

const (
	// CommitPolicySink commits offsets only when sink delivery succeeds.
	CommitPolicySink CommitPolicy = "sink"

	// CommitPolicySinkOrDLQ commits offsets when sink delivery succeeds OR
	// when failed events are durably written to DLQ.
	CommitPolicySinkOrDLQ CommitPolicy = "sink_or_dlq"

	// CommitPolicyKafkaTransaction enables Kafka transactional consume-transform-produce
	// exactly-once semantics (Kafka source + Kafka sink only).
	CommitPolicyKafkaTransaction CommitPolicy = "kafka_transaction"
)

// NormalizeCommitPolicy returns a normalized policy value.
// Empty input defaults to CommitPolicySinkOrDLQ.
func NormalizeCommitPolicy(raw string) CommitPolicy {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return CommitPolicySinkOrDLQ
	}
	return CommitPolicy(normalized)
}

// Valid returns whether the policy is supported.
func (p CommitPolicy) Valid() bool {
	switch p {
	case CommitPolicySink, CommitPolicySinkOrDLQ, CommitPolicyKafkaTransaction:
		return true
	default:
		return false
	}
}
