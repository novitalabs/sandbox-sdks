package ai.novita.sandbox.core;

import com.google.gson.JsonParser;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertEquals;

class TemplateDockerfileCommandTest {
    private static String startup(String instructions) throws Exception {
        String json = TemplateBuilder.create().fromDockerfile("FROM debian:12\n" + instructions)
                .inheritImageConfig(false).toJson();
        return JsonParser.parseString(json).getAsJsonObject().get("startCmd").getAsString();
    }

    @Test void execEntrypointReceivesShellCmdAsShellArguments() throws Exception {
        assertEquals("runner /bin/sh -c 'echo \"$HOME\" && echo done'",
                startup("ENTRYPOINT [\"runner\"]\nCMD echo \"$HOME\" && echo done\n"));
    }

    @Test void shellEntrypointIgnoresExecAndShellCmd() throws Exception {
        assertEquals("echo startup", startup("ENTRYPOINT echo startup\nCMD [\"ignored\", \"argument\"]\n"));
        assertEquals("echo startup", startup("ENTRYPOINT echo startup\nCMD echo ignored\n"));
    }

    @Test void lastEntrypointAndCmdDetermineTheirFinalForms() throws Exception {
        assertEquals("runner app.py 'a b'", startup("ENTRYPOINT echo old\nCMD echo old\n"
                + "ENTRYPOINT [\"runner\"]\nCMD [\"app.py\", \"a b\"]\n"));
        assertEquals("echo final", startup("ENTRYPOINT [\"runner\"]\nCMD [\"ignored\"]\nENTRYPOINT echo final\n"));
    }

    @Test void emptyExecEntrypointLeavesStandaloneCmdBehavior() throws Exception {
        assertEquals("echo \"$HOME\"", startup("ENTRYPOINT []\nCMD echo \"$HOME\"\n"));
        assertEquals("runner 'a b'", startup("CMD [\"runner\", \"a b\"]\n"));
    }
}
