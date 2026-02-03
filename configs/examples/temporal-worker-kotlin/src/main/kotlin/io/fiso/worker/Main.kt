package io.fiso.worker

import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowClientOptions
import io.temporal.serviceclient.WorkflowServiceStubs
import io.temporal.serviceclient.WorkflowServiceStubsOptions
import io.temporal.worker.WorkerFactory
import java.util.logging.Logger

/** Temporal worker that processes events from fiso-flow via fiso-link. */
fun main() {
    val logger = Logger.getLogger("io.fiso.worker")
    val taskQueue = "order-processing"

    val hostPort = System.getenv("TEMPORAL_HOST") ?: "localhost:7233"
    val linkAddr = System.getenv("FISO_LINK_ADDR") ?: "http://localhost:3500"

    logger.info("connecting to temporal at $hostPort")

    val service = WorkflowServiceStubs.newServiceStubs(
        WorkflowServiceStubsOptions.newBuilder()
            .setTarget(hostPort)
            .build()
    )

    val client = WorkflowClient.newInstance(
        service,
        WorkflowClientOptions.newBuilder()
            .setNamespace("default")
            .build()
    )

    val factory = WorkerFactory.newInstance(client)
    val worker = factory.newWorker(taskQueue)

    worker.registerWorkflowImplementationTypes(ProcessEventWorkflowImpl::class.java)
    worker.registerActivitiesImplementations(ExternalServiceActivityImpl(linkAddr))

    logger.info("connected to temporal, starting worker on queue=$taskQueue")
    factory.start()
}
