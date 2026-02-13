package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Attribute key constants for consistent span attributes.
const (
	AttrFlowName       = "fiso.flow.name"
	AttrCorrelationID  = "fiso.correlation_id"
	AttrKafkaTopic     = "messaging.kafka.topic"
	AttrKafkaPartition = "messaging.kafka.partition"
	AttrKafkaOffset    = "messaging.kafka.offset"
	AttrHTTPTarget     = "http.target"
	AttrHTTPMethod     = "http.method"
	AttrHTTPStatus     = "http.status_code"
	AttrGRPCMethod     = "rpc.grpc.method"
	AttrWorkflowType   = "temporal.workflow.type"
	AttrWorkflowID     = "temporal.workflow.id"
	AttrSignalName     = "temporal.signal.name"
	AttrTargetName     = "fiso.target.name"
	AttrErrorType      = "error.type"
)

// Span name constants for consistent span naming.
const (
	SpanEventReceived  = "fiso.event.receive"
	SpanTransform      = "fiso.transform"
	SpanDeliver        = "fiso.deliver"
	SpanProxyRequest   = "fiso.proxy.request"
	SpanKafkaConsume   = "kafka.consume"
	SpanKafkaPublish   = "kafka.publish"
	SpanHTTPDeliver    = "http.deliver"
	SpanGRPCDeliver    = "grpc.deliver"
	SpanTemporalInvoke = "temporal.invoke"
)

// StartSpan starts a new span with the given name and options.
// Returns the new context with the span and the span itself.
// If tracer is nil, returns a no-op span.
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, opts...)
}

// SetSpanError records an error on the span and sets the status to Error.
func SetSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanOK sets the span status to Ok.
func SetSpanOK(span trace.Span) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, "")
}

// SetSpanOKWithMessage sets the span status to Ok with a description.
func SetSpanOKWithMessage(span trace.Span, message string) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, message)
}

// Attribute constructors for common attributes.

// FlowAttr returns an attribute for the flow name.
func FlowAttr(name string) attribute.KeyValue {
	return attribute.String(AttrFlowName, name)
}

// CorrelationAttr returns an attribute for the correlation ID.
func CorrelationAttr(id string) attribute.KeyValue {
	return attribute.String(AttrCorrelationID, id)
}

// KafkaTopicAttr returns an attribute for the Kafka topic.
func KafkaTopicAttr(topic string) attribute.KeyValue {
	return attribute.String(AttrKafkaTopic, topic)
}

// KafkaPartitionAttr returns an attribute for the Kafka partition.
func KafkaPartitionAttr(partition int32) attribute.KeyValue {
	return attribute.Int64(AttrKafkaPartition, int64(partition))
}

// KafkaOffsetAttr returns an attribute for the Kafka offset.
func KafkaOffsetAttr(offset int64) attribute.KeyValue {
	return attribute.Int64(AttrKafkaOffset, offset)
}

// HTTPTargetAttr returns an attribute for the HTTP target URL.
func HTTPTargetAttr(url string) attribute.KeyValue {
	return attribute.String(AttrHTTPTarget, url)
}

// HTTPMethodAttr returns an attribute for the HTTP method.
func HTTPMethodAttr(method string) attribute.KeyValue {
	return attribute.String(AttrHTTPMethod, method)
}

// HTTPStatusAttr returns an attribute for the HTTP status code.
func HTTPStatusAttr(status int) attribute.KeyValue {
	return attribute.Int(AttrHTTPStatus, status)
}

// GRPCMethodAttr returns an attribute for the gRPC method.
func GRPCMethodAttr(method string) attribute.KeyValue {
	return attribute.String(AttrGRPCMethod, method)
}

// WorkflowTypeAttr returns an attribute for the Temporal workflow type.
func WorkflowTypeAttr(workflowType string) attribute.KeyValue {
	return attribute.String(AttrWorkflowType, workflowType)
}

// WorkflowIDAttr returns an attribute for the Temporal workflow ID.
func WorkflowIDAttr(id string) attribute.KeyValue {
	return attribute.String(AttrWorkflowID, id)
}

// SignalNameAttr returns an attribute for the Temporal signal name.
func SignalNameAttr(name string) attribute.KeyValue {
	return attribute.String(AttrSignalName, name)
}

// TargetNameAttr returns an attribute for the target name.
func TargetNameAttr(name string) attribute.KeyValue {
	return attribute.String(AttrTargetName, name)
}

// ErrorTypeAttr returns an attribute for the error type.
func ErrorTypeAttr(errType string) attribute.KeyValue {
	return attribute.String(AttrErrorType, errType)
}

// SpanFromContext returns the current span from the context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// IsTraced returns true if there is a valid recording span in the context.
func IsTraced(ctx context.Context) bool {
	span := trace.SpanFromContext(ctx)
	return span.SpanContext().IsValid() && span.IsRecording()
}
