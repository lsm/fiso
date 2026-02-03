package io.fiso.worker

import io.temporal.activity.ActivityOptions
import io.temporal.common.RetryOptions
import io.temporal.workflow.Workflow
import io.temporal.workflow.WorkflowInterface
import io.temporal.workflow.WorkflowMethod
import java.time.Duration

/** Temporal workflow interface for processing events from fiso-flow. */
@WorkflowInterface
interface ProcessEventWorkflow {
    @WorkflowMethod
    fun processEvent(input: ByteArray): String
}

/**
 * Workflow that processes a single event by calling an external service via fiso-link.
 */
class ProcessEventWorkflowImpl : ProcessEventWorkflow {

    private val activity: ExternalServiceActivity = Workflow.newActivityStub(
        ExternalServiceActivity::class.java,
        ActivityOptions.newBuilder()
            .setStartToCloseTimeout(Duration.ofSeconds(30))
            .setRetryOptions(
                RetryOptions.newBuilder()
                    .setMaximumAttempts(3)
                    .build()
            )
            .build()
    )

    override fun processEvent(input: ByteArray): String {
        return activity.callExternalService(input)
    }
}
