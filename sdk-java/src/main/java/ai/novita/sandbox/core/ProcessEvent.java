package ai.novita.sandbox.core;

import com.google.gson.*;
import java.nio.charset.StandardCharsets;
import java.util.Base64;

/** Decoded process.v1 oneof event. */
public final class ProcessEvent {
    private String type, status, error;
    private int pid, exitCode;
    private byte[] stdout = new byte[0], stderr = new byte[0], pty = new byte[0];
    public String getType(){ return type; }
    public String getStatus(){ return status; }
    public String getError(){ return error; }
    public int getPid(){ return pid; }
    public int getExitCode(){ return exitCode; }
    public byte[] getStdoutBytes(){ return stdout.clone(); }
    public byte[] getStderrBytes(){ return stderr.clone(); }
    public byte[] getPtyBytes(){ return pty.clone(); }
    public String getStdout(){ return new String(stdout, StandardCharsets.UTF_8); }
    public String getStderr(){ return new String(stderr, StandardCharsets.UTF_8); }
    public String getPty(){ return new String(pty, StandardCharsets.UTF_8); }

    /** Parses Connect JSON, including the nested event and base64 bytes. */
    public static ProcessEvent parse(byte[] payload) {
        ProcessEvent out = new ProcessEvent();
        JsonObject wrapper = JsonParser.parseString(new String(payload, StandardCharsets.UTF_8)).getAsJsonObject();
        JsonObject e = wrapper.has("event") ? wrapper.getAsJsonObject("event") : wrapper;
        if (e.has("start")) { out.type="start"; out.pid=e.getAsJsonObject("start").get("pid").getAsInt(); }
        else if (e.has("data")) {
            out.type="data"; JsonObject d=e.getAsJsonObject("data");
            if(d.has("stdout")) out.stdout=decode(d.get("stdout"));
            if(d.has("stderr")) out.stderr=decode(d.get("stderr"));
            if(d.has("pty")) out.pty=decode(d.get("pty"));
        } else if (e.has("end")) {
            out.type="end";
            JsonObject x=e.getAsJsonObject("end");
            if (x.has("exitCode")) out.exitCode=x.get("exitCode").getAsInt();
            else if (x.has("exit_code")) out.exitCode=x.get("exit_code").getAsInt();
            if(x.has("status"))out.status=x.get("status").getAsString();
            if(x.has("error"))out.error=x.get("error").getAsString();
        }
        else if(e.has("keepalive")) out.type="keepalive";
        return out;
    }
    private static byte[] decode(JsonElement x) { if(x.isJsonNull()) return new byte[0]; try{return Base64.getDecoder().decode(x.getAsString());}catch(IllegalArgumentException ex){return x.getAsString().getBytes(StandardCharsets.UTF_8);} }
}
