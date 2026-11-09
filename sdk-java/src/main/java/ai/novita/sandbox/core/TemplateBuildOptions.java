package ai.novita.sandbox.core;

import java.util.*;

public class TemplateBuildOptions {
    private String alias; private List<String> tags = new ArrayList<>(); private Map<String,Object> specification = new HashMap<>(); private String fileContextPath = "."; private int cpuCount = 2; private int memoryMb = 1024; private long timeoutSeconds = 1800; private long pollIntervalMillis = 1000; private java.util.function.Consumer<BuildLogEntry> logConsumer;
    public String getAlias() { return alias; } public TemplateBuildOptions setAlias(String v) { alias=v; return this; }
    /** Returns an unmodifiable view so callers cannot mutate the option in place. */
    public List<String> getTags() { return Collections.unmodifiableList(tags); } public TemplateBuildOptions setTags(List<String> v) { tags=v==null?new ArrayList<>():new ArrayList<>(v); return this; }
    /** Returns an unmodifiable view so callers cannot mutate the option in place. */
    public Map<String,Object> getSpecification() { return Collections.unmodifiableMap(specification); } public TemplateBuildOptions setSpecification(Map<String,Object> v) { specification=v==null?new HashMap<>():new HashMap<>(v); return this; }
    public int getCpuCount() { return cpuCount; } public TemplateBuildOptions setCpuCount(int v) { cpuCount=v; return this; }
    public String getFileContextPath() { return fileContextPath; } public TemplateBuildOptions setFileContextPath(String v) { if (v == null || v.trim().isEmpty()) throw new IllegalArgumentException("fileContextPath is required"); fileContextPath=v; return this; }
    public int getMemoryMb() { return memoryMb; } public TemplateBuildOptions setMemoryMb(int v) { memoryMb=v; return this; }
    public long getTimeoutSeconds(){return timeoutSeconds;} public TemplateBuildOptions setTimeoutSeconds(long v){if(v<=0)throw new IllegalArgumentException("timeoutSeconds must be positive");timeoutSeconds=v;return this;}
    public long getPollIntervalMillis(){return pollIntervalMillis;} public TemplateBuildOptions setPollIntervalMillis(long v){if(v<=0)throw new IllegalArgumentException("pollIntervalMillis must be positive");pollIntervalMillis=v;return this;}
    public java.util.function.Consumer<BuildLogEntry> getLogConsumer(){return logConsumer;} public TemplateBuildOptions setLogConsumer(java.util.function.Consumer<BuildLogEntry> v){logConsumer=v;return this;}
    /**
     * The v2 trigger payload, matching JS {@code serialize()}: only the build
     * specification. Alias and tags belong to the /v3/templates allocate
     * request, never to the trigger body.
     */
    Map<String,Object> toMap() { return new HashMap<>(specification); }

    /** Defensive copy so callers' option objects are never mutated by the SDK. */
    public TemplateBuildOptions copy() {
        TemplateBuildOptions c = new TemplateBuildOptions();
        c.alias = alias;
        c.tags = new ArrayList<>(tags);
        c.specification = new HashMap<>(specification);
        c.fileContextPath = fileContextPath;
        c.cpuCount = cpuCount;
        c.memoryMb = memoryMb;
        c.timeoutSeconds = timeoutSeconds;
        c.pollIntervalMillis = pollIntervalMillis;
        c.logConsumer = logConsumer;
        return c;
    }
}
