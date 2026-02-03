package io.fiso.worker;

import io.temporal.workflow.WorkflowInterface;
import io.temporal.workflow.WorkflowMethod;

/** Temporal workflow interface for processing events from fiso-flow. */
@WorkflowInterface
public interface ProcessEventWorkflow {

    @WorkflowMethod
    String processEvent(byte[] input);
}
