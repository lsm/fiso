package io.fiso.worker;

import io.temporal.activity.ActivityOptions;
import io.temporal.common.RetryOptions;
import io.temporal.workflow.Workflow;
import java.time.Duration;

/**
 * Workflow that processes a single event by calling an external service via fiso-link.
 */
public class ProcessEventWorkflowImpl implements ProcessEventWorkflow {

    private final ExternalServiceActivity activity =
            Workflow.newActivityStub(
                    ExternalServiceActivity.class,
                    ActivityOptions.newBuilder()
                            .setStartToCloseTimeout(Duration.ofSeconds(30))
                            .setRetryOptions(
                                    RetryOptions.newBuilder().setMaximumAttempts(3).build())
                            .build());

    @Override
    public String processEvent(byte[] input) {
        return activity.callExternalService(input);
    }
}
