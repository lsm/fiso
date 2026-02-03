"""Temporal worker that processes events from fiso-flow via fiso-link."""

import json
import logging
import os
from dataclasses import dataclass
from datetime import timedelta

import httpx
from temporalio import activity, workflow
from temporalio.client import Client
from temporalio.worker import Worker

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

TASK_QUEUE = "order-processing"
LINK_ADDR = os.getenv("FISO_LINK_ADDR", "http://localhost:3500")


@activity.defn
async def call_external_service(input_data: bytes) -> str:
    """Call an external API through fiso-link.

    The input is a CloudEvent-wrapped payload from fiso-flow.
    We extract the data field and forward it via the fiso-link proxy.
    """
    cloud_event = json.loads(input_data)
    data = json.dumps(cloud_event.get("data", {}))
    logger.info("calling fiso-link with data: %s", data)

    async with httpx.AsyncClient() as client:
        resp = await client.post(
            f"{LINK_ADDR}/link/echo",
            content=data,
            headers={"Content-Type": "application/json"},
        )

    body = resp.text
    logger.info("WORKFLOW_COMPLETE external_response=%s", body)

    if resp.status_code >= 400:
        raise RuntimeError(f"fiso-link returned {resp.status_code}: {body}")

    return body


@workflow.defn
class ProcessEvent:
    """Workflow that processes a single event by calling an external service."""

    @workflow.run
    async def run(self, input_data: bytes) -> str:
        return await workflow.execute_activity(
            call_external_service,
            input_data,
            start_to_close_timeout=timedelta(seconds=30),
            retry_policy=workflow.RetryPolicy(maximum_attempts=3),
        )


async def main():
    host_port = os.getenv("TEMPORAL_HOST", "localhost:7233")
    logger.info("connecting to temporal at %s", host_port)

    client = await Client.connect(host_port, namespace="default")
    logger.info("connected to temporal, starting worker on queue=%s", TASK_QUEUE)

    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[ProcessEvent],
        activities=[call_external_service],
    )
    await worker.run()


if __name__ == "__main__":
    import asyncio

    asyncio.run(main())
