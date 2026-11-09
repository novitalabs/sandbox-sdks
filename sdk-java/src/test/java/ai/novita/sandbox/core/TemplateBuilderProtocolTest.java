package ai.novita.sandbox.core;

import org.junit.jupiter.api.Test;

import java.util.Arrays;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Wire-format tests for {@link TemplateBuilder}: Dockerfile parsing and the
 * instruction serialization shared with the JS/Python template builders.
 */
class TemplateBuilderProtocolTest {

    @Test
    void dockerfileParserMapsCoreInstructions() throws Exception {
        TemplateBuilder builder = TemplateBuilder.create().fromDockerfile(
                "FROM debian:12\n"
                        + "RUN apt-get update\n"
                        + "COPY app /app\n"
                        + "WORKDIR /app\n"
                        + "USER sandbox\n"
                        + "ENV KEY=value\n"
                        + "CMD [\"python\", \"server.py\"]\n");

        Map<String, Object> spec = builder.toMap();
        assertEquals("debian:12", spec.get("fromImage"));
        assertEquals("python server.py", spec.get("startCmd"));

        List<Map<String, Object>> steps = steps(spec);
        // Capture the base environment, then seed Docker defaults and map the
        // file in order. Persist ENV after the caller instructions.
        assertEquals(Arrays.asList("root"), steps.get(1).get("args"));
        assertEquals(Arrays.asList("/"), steps.get(2).get("args"));
        assertEquals(Arrays.asList("apt-get update"), steps.get(3).get("args"));
        assertEquals(Arrays.asList("app", "/app", "", ""), steps.get(4).get("args"));
        assertEquals(Arrays.asList("/app"), steps.get(5).get("args"));
        assertEquals(Arrays.asList("sandbox"), steps.get(6).get("args"));
        assertEquals(Arrays.asList("KEY", "value"), steps.get(7).get("args"));
        assertEquals("RUN", steps.get(steps.size() - 1).get("type"));
    }

    @Test
    void dockerfileParserAppliesSandboxDefaults() throws Exception {
        Map<String, Object> spec = TemplateBuilder.create().fromDockerfile("FROM python:3\n")
                .toMap();

        List<Map<String, Object>> steps = steps(spec);
        // JS/Python default to user "user" and workdir /home/user when the
        // Dockerfile sets neither.
        assertEquals(Arrays.asList("root"), steps.get(0).get("args"));
        assertEquals(Arrays.asList("/"), steps.get(1).get("args"));
        assertEquals(Arrays.asList("user"), steps.get(steps.size() - 2).get("args"));
        assertEquals(Arrays.asList("/home/user"), steps.get(steps.size() - 1).get("args"));
    }

    @Test
    void dockerfileParserRejectsMultiStageBuilds() {
        assertThrows(BuildException.class, () -> TemplateBuilder.create()
                .fromDockerfile("FROM debian:12 AS build\nFROM debian:12\n"));
    }

    @Test
    void dockerfileParserJoinsContinuationLines() throws Exception {
        Map<String, Object> spec = TemplateBuilder.create().fromDockerfile(
                "FROM debian:12\nRUN apt-get update && \\\n    apt-get install -y curl\n")
                .toMap();

        // The backslash becomes a space and the indented continuation is
        // trimmed, producing one logical RUN instruction.
        assertEquals(Arrays.asList("apt-get update &&  apt-get install -y curl"),
                steps(spec).get(2).get("args"));
    }

    @Test
    void runInstructionEncodesUserAsSecondArg() {
        Map<String, Object> spec = TemplateBuilder.create()
                .fromImage("debian:12")
                .run("echo hi")
                .run("whoami", "root")
                .toMap();

        List<Map<String, Object>> steps = steps(spec);
        assertEquals(Arrays.asList("echo hi"), steps.get(0).get("args"));
        // JS/Python always serialize the force flag, false included.
        assertEquals(false, steps.get(0).get("force"));
        assertEquals(Arrays.asList("whoami", "root"), steps.get(1).get("args"));
    }

    @Test
    void copyInstructionMatchesWireFormat() {
        Map<String, Object> spec = TemplateBuilder.create()
                .fromImage("debian:12")
                .copyFile("src", "/dest", "root", 0755, true, true)
                .toMap();

        Map<String, Object> copy = steps(spec).get(0);
        assertEquals("COPY", copy.get("type"));
        // JS: args = [src, dest, user ?? '', mode ? padOctal(mode) : ''].
        assertEquals(Arrays.asList("src", "/dest", "root", "0755"), copy.get("args"));
        assertEquals(true, copy.get("force"));
        assertEquals(true, copy.get("forceUpload"));
        assertEquals(true, copy.get("resolveSymlinks"));
    }

    @Test
    void skipCacheForcesSubsequentLayers() {
        Map<String, Object> spec = TemplateBuilder.create()
                .fromImage("debian:12")
                .run("echo one")
                .skipCache()
                .run("echo two")
                .toMap();

        List<Map<String, Object>> steps = steps(spec);
        // force is always serialized; skipCache only flips it to true.
        assertEquals(false, steps.get(0).get("force"));
        assertEquals(true, steps.get(1).get("force"));
        assertEquals(true, spec.get("force"));
    }

    @Test
    void patchCommandSerializesAsPreBootScript() {
        // AGENTS.md: set_patch_cmd() emits the request field preBootScript.
        Map<String, Object> spec = TemplateBuilder.create()
                .fromImage("debian:stable")
                .setPatchCmd("apt-get install -y supervisor")
                .toMap();

        assertEquals("apt-get install -y supervisor", spec.get("preBootScript"));
    }

    @Test
    void environmentsPopulateEnvVarsAndRuntimeScript() {
        Map<String, Object> spec = TemplateBuilder.create()
                .fromImage("debian:12")
                .environment("FOO", "bar", true)
                .toMap();

        // JS/Python setEnvs: an ENV build instruction, no top-level envVars
        // field on the trigger body.
        assertFalse(spec.containsKey("envVars"));
        Map<String, Object> envStep = steps(spec).get(1);
        assertEquals("ENV", envStep.get("type"));
        assertEquals(Arrays.asList("FOO", "bar"), envStep.get("args"));
        // The last step persists the env into /etc/profile.d, matching JS.
        Map<String, Object> last = steps(spec).get(steps(spec).size() - 1);
        assertEquals("RUN", last.get("type"));
        String command = ((List<String>) last.get("args")).get(0);
        assertTrue(command.contains("/tmp/.novita-image-env.sh"));
        assertEquals("root", ((List<String>) last.get("args")).get(1));
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> steps(Map<String, Object> spec) {
        return (List<Map<String, Object>>) spec.get("steps");
    }
}
