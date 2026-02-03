package io.fiso.worker;

import io.temporal.activity.ActivityInterface;
import io.temporal.activity.ActivityMethod;

/** Activity interface for calling external services through fiso-link. */
@ActivityInterface
public interface ExternalServiceActivity {

    @ActivityMethod
    String callExternalService(byte[] input);
}
