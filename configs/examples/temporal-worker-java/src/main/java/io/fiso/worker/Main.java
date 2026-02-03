package io.fiso.worker;

import io.temporal.client.WorkflowClient;
import io.temporal.client.WorkflowClientOptions;
import io.temporal.serviceclient.WorkflowServiceStubs;
import io.temporal.serviceclient.WorkflowServiceStubsOptions;
import io.temporal.worker.Worker;
import io.temporal.worker.WorkerFactory;
import java.util.logging.Logger;

/** Temporal worker that processes events from fiso-flow via fiso-link. */
public class Main {

    private static final Logger logger = Logger.getLogger(Main.class.getName());
    private static final String TASK_QUEUE = "order-processing";

    public static void main(String[] args) {
        String hostPort = System.getenv().getOrDefault("TEMPORAL_HOST", "localhost:7233");
        String linkAddr =
                System.getenv().getOrDefault("FISO_LINK_ADDR", "http://localhost:3500");

        logger.info("connecting to temporal at " + hostPort);

        WorkflowServiceStubs service =
                WorkflowServiceStubs.newServiceStubs(
                        WorkflowServiceStubsOptions.newBuilder()
                                .setTarget(hostPort)
                                .build());

        WorkflowClient client =
                WorkflowClient.newInstance(
                        service,
                        WorkflowClientOptions.newBuilder()
                                .setNamespace("default")
                                .build());

        WorkerFactory factory = WorkerFactory.newInstance(client);
        Worker worker = factory.newWorker(TASK_QUEUE);

        worker.registerWorkflowImplementationTypes(ProcessEventWorkflowImpl.class);
        worker.registerActivitiesImplementations(new ExternalServiceActivityImpl(linkAddr));

        logger.info("connected to temporal, starting worker on queue=" + TASK_QUEUE);
        factory.start();
    }
}
