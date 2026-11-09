package ai.novita.sandbox.core;

import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;

import static org.junit.jupiter.api.Assertions.assertEquals;

class ProcessEventTest {
    @Test
    void readsConnectJsonStartAndSnakeCaseExitCode() {
        ProcessEvent start = ProcessEvent.parse(
                "{\"event\":{\"start\":{\"pid\":42}}}".getBytes(StandardCharsets.UTF_8));
        ProcessEvent end = ProcessEvent.parse(
                "{\"event\":{\"end\":{\"exit_code\":7}}}".getBytes(StandardCharsets.UTF_8));

        assertEquals("start", start.getType());
        assertEquals(42, start.getPid());
        assertEquals("end", end.getType());
        assertEquals(7, end.getExitCode());
    }
}
