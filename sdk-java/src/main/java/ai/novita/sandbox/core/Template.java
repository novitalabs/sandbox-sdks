package ai.novita.sandbox.core;

import java.util.List;
import java.util.Map;
import java.nio.file.*; import java.security.*; import java.io.*; import okhttp3.*; import com.google.gson.JsonObject;

/** Control-plane operations for reusable sandbox templates. */
public final class Template {
    private final ApiClient client;
    Template(ApiClient client) { this.client = client; }
    public TemplateListResult list() throws SandboxException { return list(new TemplateListOptions().setTemplateType("template_build").setPage(1).setLimit(20)); }
    public java.util.concurrent.CompletableFuture<TemplateListResult> listAsync(){return AsyncSupport.supplyAsync(()->{try{return list();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public TemplateListResult list(TemplateListOptions options) throws SandboxException {
        rejectLegacyTemplateApi("Template.list");
        TemplateListOptions effective = options == null ? new TemplateListOptions() : options;
        String type = effective.getTemplateType() == null ? "template_build" : effective.getTemplateType();
        int page = effective.getPage() == 0 ? 1 : effective.getPage();
        int limit = effective.getLimit() == 0 ? 20 : effective.getLimit();
        if (!"template_build".equals(type) && !"snapshot_template".equals(type))
            throw new IllegalArgumentException("templateType must be either template_build or snapshot_template");
        if (page < 1) throw new IllegalArgumentException("page must be >= 1");
        if (limit < 1 || limit > 100) throw new IllegalArgumentException("limit must be between 1 and 100");
        java.util.Map<String,String> q=new java.util.LinkedHashMap<>();
        q.put("template_type", type); q.put("page", String.valueOf(page)); q.put("limit", String.valueOf(limit));
        return client.request("GET","/v2/templates",q,null,TemplateListResult.class);
    }
    /**
     * Fetches a template, matching JS/Python: derives {@code buildID} and
     * {@code buildStatus} from the first entry of the {@code builds} array.
     */
    public TemplateInfo get(String templateId) throws SandboxException {
        require(templateId);
        rejectLegacyTemplateApi("Template.get");
        TemplateInfo info = client.request("GET", "/templates/" + enc(templateId), null, TemplateInfo.class);
        if (info != null) info.deriveLatestBuild();
        return info;
    }
    public java.util.concurrent.CompletableFuture<TemplateInfo> getAsync(String id){return AsyncSupport.supplyAsync(()->{try{return get(id);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public boolean delete(String templateId) throws SandboxException {
        require(templateId);
        rejectLegacyTemplateApi("Template.delete");
        try { client.request("DELETE", "/templates/" + enc(templateId), null, null); return true; } catch (NotFoundException e) { return false; }
    }
    public java.util.concurrent.CompletableFuture<Boolean> deleteAsync(String id){return AsyncSupport.supplyAsync(()->{try{return delete(id);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public BuildInfo build(String name, TemplateBuildOptions options) throws SandboxException {
        if (name == null || name.isEmpty()) throw new BuildException("name is required");
        if (options != null && (options.getCpuCount() <= 0 || options.getMemoryMb() <= 0)) throw new BuildException("cpuCount and memoryMb must be positive");
        rejectLegacyTemplateApi("Template.build");
        BuildInfo info = client.requestBuild(name, options);
        Map<String,Object> spec = normalizedTriggerSpec(options);
        uploadCopyInstructions(info, spec, options == null ? "." : options.getFileContextPath());
        client.triggerBuild(info.getTemplateId(), info.getBuildId(), spec);
        waitForBuild(info, options);
        return info;
    }

    /**
     * Builds without waiting for completion, matching JS/Python
     * {@code buildInBackground}: allocate, upload and trigger only.
     * Poll {@link #getBuildStatus(String, String)} for progress.
     */
    public BuildInfo buildInBackground(String name, TemplateBuildOptions options) throws SandboxException {
        if (name == null || name.isEmpty()) throw new BuildException("name is required");
        if (options != null && (options.getCpuCount() <= 0 || options.getMemoryMb() <= 0)) throw new BuildException("cpuCount and memoryMb must be positive");
        rejectLegacyTemplateApi("Template.buildInBackground");
        BuildInfo info = client.requestBuild(name, options);
        Map<String,Object> spec = normalizedTriggerSpec(options);
        uploadCopyInstructions(info, spec, options == null ? "." : options.getFileContextPath());
        client.triggerBuild(info.getTemplateId(), info.getBuildId(), spec);
        return info;
    }
    public java.util.concurrent.CompletableFuture<BuildInfo> buildInBackgroundAsync(String name, TemplateBuildOptions options){return AsyncSupport.supplyAsync(()->{try{return buildInBackground(name,options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    /**
     * The trigger payload, matching JS/Python {@code serialize()}: {@code steps}
     * and {@code force} are always present, even for an empty build.
     */
    private static Map<String,Object> normalizedTriggerSpec(TemplateBuildOptions options) {
        Map<String,Object> spec = options == null ? new java.util.LinkedHashMap<>() : options.toMap();
        spec.putIfAbsent("steps", new java.util.ArrayList<>());
        spec.putIfAbsent("force", false);
        return spec;
    }

    private void uploadCopyInstructions(BuildInfo info, Map<String,Object> spec, String contextPath) throws SandboxException {
        Object raw = spec.get("steps"); if (!(raw instanceof Iterable)) return;
        for (Object item : (Iterable<?>) raw) { if (!(item instanceof Map)) continue; Map<String,Object> i=(Map<String,Object>)item; if (!"COPY".equals(i.get("type"))) continue; Object a=i.get("args"); if (!(a instanceof List) || ((List<?>)a).size()<2) continue; String source=String.valueOf(((List<?>)a).get(0)); validateCopySource(source); Path path=Paths.get(contextPath).resolve(source).normalize(); if(!Files.exists(path, LinkOption.NOFOLLOW_LINKS)) throw new BuildException("copy source does not exist: " + path); boolean force=Boolean.TRUE.equals(i.get("forceUpload")) || Boolean.TRUE.equals(i.get("force")); try { boolean follow=Boolean.TRUE.equals(i.get("resolveSymlinks")); java.util.List<String> ignores=new java.util.ArrayList<>(); Path ignoreFile=Paths.get(contextPath).resolve(".dockerignore"); if(Files.isRegularFile(ignoreFile)) ignores.addAll(Files.readAllLines(ignoreFile)); byte[] tar=tar(path, contextPath, follow, ignores); String hash=filesHash(path, source, String.valueOf(((List<?>)a).get(1)), contextPath, follow, ignores); i.put("filesHash", hash); UploadLink link=client.request("GET", "/templates/"+enc(info.getTemplateId())+"/files/"+hash, null, UploadLink.class); if (link != null && link.getUrl() != null && (force || !link.isPresent())) put(link.getUrl(),tar); } catch(IOException|GeneralSecurityException e){throw new BuildException("failed to upload " + path,e);} }
    }

    /** Uploads to a presigned URL using the shared client (no API key header). */
    private void put(String url, byte[] data) throws IOException {
        Request r=new Request.Builder().url(url).put(RequestBody.create(data, MediaType.get("application/x-tar"))).build();
        try(Response x=AsyncSupport.execute(client.httpClient().newCall(r))){if(!x.isSuccessful())throw new IOException("upload HTTP "+x.code());}
    }
    private static String sha256(byte[] b) throws GeneralSecurityException { byte[] d=MessageDigest.getInstance("SHA-256").digest(b); StringBuilder s=new StringBuilder(); for(byte x:d)s.append(String.format("%02x",x)); return s.toString(); }
    private static String filesHash(Path src, String source, String dest, String contextPath, boolean follow, java.util.List<String> ignores) throws IOException, GeneralSecurityException {
        MessageDigest h=MessageDigest.getInstance("SHA-256"); h.update(("COPY "+source+" "+dest).getBytes(java.nio.charset.StandardCharsets.UTF_8));
        Path root=src.toAbsolutePath().normalize(); Path base=java.nio.file.Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize();
        base=java.nio.file.Paths.get(contextPath).toAbsolutePath().normalize();
        java.util.List<Path> entries=new java.util.ArrayList<>(); try(java.util.stream.Stream<Path> stream=Files.walk(root, follow ? new FileVisitOption[]{FileVisitOption.FOLLOW_LINKS} : new FileVisitOption[0])){stream.sorted().forEach(entries::add);}
        if(entries.isEmpty()) throw new IOException("No files found in "+src);
        for(Path p:entries){Path rel; try{rel=base.relativize(p.toAbsolutePath().normalize());}catch(IllegalArgumentException e){rel=p.getFileName();} String relName=rel.toString().replace(java.io.File.separatorChar,'/'); if(ignored(relName, ignores)) continue; h.update(relName.getBytes(java.nio.charset.StandardCharsets.UTF_8)); java.nio.file.attribute.BasicFileAttributes a=follow?Files.readAttributes(p,java.nio.file.attribute.BasicFileAttributes.class):Files.readAttributes(p,java.nio.file.attribute.BasicFileAttributes.class,java.nio.file.LinkOption.NOFOLLOW_LINKS); Object mode=0; try{mode=Files.getAttribute(p,"unix:mode",follow ? new LinkOption[0] : new LinkOption[]{LinkOption.NOFOLLOW_LINKS});}catch(Exception ignored){} h.update(String.valueOf(mode).getBytes(java.nio.charset.StandardCharsets.UTF_8)); h.update(Long.toString(a.size()).getBytes(java.nio.charset.StandardCharsets.UTF_8)); if(Files.isSymbolicLink(p)&&!follow){h.update(Files.readSymbolicLink(p).toString().getBytes(java.nio.charset.StandardCharsets.UTF_8));} else if(a.isRegularFile()){h.update(Files.readAllBytes(p));}}
        byte[] digest=h.digest(); StringBuilder s=new StringBuilder(); for(byte x:digest)s.append(String.format("%02x",x)); return s.toString();
    }

    private static byte[] tar(Path p) throws IOException { return tar(p, ".", false, java.util.Collections.emptyList()); }
    private static byte[] tar(Path p, boolean followSymlinks) throws IOException { return tar(p, ".", followSymlinks, java.util.Collections.emptyList()); }
    private static byte[] tar(Path p, String contextPath, boolean followSymlinks, java.util.List<String> ignores) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        final java.util.List<String> ignoreRules = ignores;
        final Path base = Paths.get(contextPath).toAbsolutePath().normalize();
        try (java.util.zip.GZIPOutputStream gz = new java.util.zip.GZIPOutputStream(out);
             org.apache.commons.compress.archivers.tar.TarArchiveOutputStream t =
                     new org.apache.commons.compress.archivers.tar.TarArchiveOutputStream(gz)) {
            t.setLongFileMode(org.apache.commons.compress.archivers.tar.TarArchiveOutputStream.LONGFILE_POSIX);
            Path source = p.toAbsolutePath().normalize();
            if (!source.startsWith(base)) throw new IOException("COPY source is outside file context");
            try (java.util.stream.Stream<Path> walk = Files.walk(source,
                    followSymlinks ? new FileVisitOption[]{FileVisitOption.FOLLOW_LINKS} : new FileVisitOption[0])) {
                java.util.Iterator<Path> paths = walk.sorted().iterator();
                while (paths.hasNext()) {
                    AsyncSupport.throwIfCancelled();
                    Path x = paths.next();
                    String rel = base.relativize(x).toString().replace(File.separatorChar, '/');
                    if (rel.isEmpty()) rel = ".";
                    if (ignored(rel, ignoreRules)) continue;
                    boolean preserveLink = Files.isSymbolicLink(x) && !followSymlinks;
                    org.apache.commons.compress.archivers.tar.TarArchiveEntry entry;
                    if (preserveLink) {
                        entry = new org.apache.commons.compress.archivers.tar.TarArchiveEntry(rel,
                                org.apache.commons.compress.archivers.tar.TarConstants.LF_SYMLINK);
                        entry.setLinkName(Files.readSymbolicLink(x).toString().replace(File.separatorChar, '/'));
                    } else {
                        entry = new org.apache.commons.compress.archivers.tar.TarArchiveEntry(x.toFile(), rel);
                    }
                    t.putArchiveEntry(entry);
                    if (!preserveLink && Files.isRegularFile(x)) Files.copy(x, t);
                    t.closeArchiveEntry();
                }
            } catch (UncheckedIOException e) { throw e.getCause(); }
            t.finish();
        }
        return out.toByteArray();
    }
    private static boolean ignored(String path, java.util.List<String> patterns){
        boolean ignored=false;
        String normalized=path.replace('\\','/');
        for(String raw:patterns){
            String s=raw.trim();
            if(s.isEmpty()||s.startsWith("#")) continue;
            boolean neg=s.startsWith("!");
            if(neg) s=s.substring(1).trim();
            s=s.replace('\\','/');
            boolean directory=s.endsWith("/");
            if(directory) s=s.substring(0,s.length()-1);
            boolean anchored=s.startsWith("/");
            if(anchored) s=s.substring(1);
            boolean match=false;
            try {
                java.nio.file.Path candidate=java.nio.file.Paths.get(normalized);
                java.nio.file.Path pattern=java.nio.file.Paths.get(s);
                match=java.nio.file.FileSystems.getDefault().getPathMatcher("glob:"+s).matches(candidate);
                if(!anchored && !match && normalized.contains("/")) {
                    for(String part: normalized.split("/")) {
                        if(java.nio.file.FileSystems.getDefault().getPathMatcher("glob:"+s).matches(java.nio.file.Paths.get(part))) { match=true; break; }
                    }
                }
            } catch(RuntimeException ignoredPattern) { match=normalized.equals(s); }
            if(!match && (normalized.equals(s)||normalized.startsWith(s+"/"))) match=true;
            if(!match && directory && normalized.startsWith(s+"/")) match=true;
            if(match) ignored=!neg;
        }
        return ignored;
    }
    private void waitForBuild(BuildInfo info, TemplateBuildOptions o) throws SandboxException { long deadline=System.nanoTime()+java.util.concurrent.TimeUnit.SECONDS.toNanos(o==null?1800:o.getTimeoutSeconds()); int offset=0; String lastStatus=null; while(System.nanoTime()<deadline){AsyncSupport.throwIfCancelled();BuildStatus s=client.request("GET","/templates/"+enc(info.getTemplateId())+"/builds/"+enc(info.getBuildId())+"/status",java.util.Collections.singletonMap("logsOffset",String.valueOf(offset)),null,BuildStatus.class); lastStatus=s.getStatus(); if(s.getLogEntries()!=null){offset+=s.getLogEntries().size(); if(o!=null&&o.getLogConsumer()!=null)s.getLogEntries().forEach(o.getLogConsumer());} String normalized=lastStatus==null?"":lastStatus.toLowerCase(java.util.Locale.ROOT); if("ready".equals(normalized)||"completed".equals(normalized)||"success".equals(normalized)||"succeeded".equals(normalized))return; if("error".equals(normalized)||"failed".equals(normalized))throw new BuildException(s.getReason()!=null?s.getReason().getMessage():s.getMessage()); try{Thread.sleep(o==null?1000:o.getPollIntervalMillis());}catch(InterruptedException e){Thread.currentThread().interrupt();throw new BuildException("build interrupted",e);}} throw new BuildException("template build timed out (last status: " + lastStatus + ")"); }
    private static class UploadLink { String url; @com.google.gson.annotations.SerializedName("upload_url") String uploadUrl; boolean present; @com.google.gson.annotations.SerializedName("already_uploaded") boolean alreadyUploaded; String getUrl(){return url!=null?url:uploadUrl;} boolean isPresent(){return present||alreadyUploaded;} }
    public BuildInfo build(String name, TemplateBuilder builder, TemplateBuildOptions options) throws SandboxException {
        if (builder == null) throw new BuildException("builder is required");
        // Copy rather than mutate the caller's options object.
        TemplateBuildOptions opts = options == null ? new TemplateBuildOptions() : options.copy();
        builder.resolveInheritedConfig(opts.getLogConsumer());
        opts.setSpecification(builder.toMap());
        opts.setFileContextPath(builder.getFileContextPath());
        return build(name, opts);
    }
    public BuildInfo buildInBackground(String name, TemplateBuilder builder, TemplateBuildOptions options) throws SandboxException {
        if (builder == null) throw new BuildException("builder is required");
        TemplateBuildOptions opts = options == null ? new TemplateBuildOptions() : options.copy();
        builder.resolveInheritedConfig(opts.getLogConsumer());
        opts.setSpecification(builder.toMap()); opts.setFileContextPath(builder.getFileContextPath());
        return buildInBackground(name, opts);
    }
    public java.util.concurrent.CompletableFuture<BuildInfo> buildInBackgroundAsync(String name, TemplateBuilder builder, TemplateBuildOptions options) {
        return AsyncSupport.supplyAsync(() -> { try { return buildInBackground(name, builder, options); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public BuildStatus getBuildStatus(String templateId, String buildId) throws SandboxException { return getBuildStatus(templateId, buildId, 0); }
    /** Read build status, returning only logs at or after the supplied offset. */
    public BuildStatus getBuildStatus(String templateId, String buildId, int logsOffset) throws SandboxException {
        require(templateId); require(buildId);
        if (logsOffset < 0) throw new IllegalArgumentException("logsOffset must not be negative");
        rejectLegacyTemplateApi("Template.getBuildStatus");
        return client.request("GET", "/templates/" + enc(templateId) + "/builds/" + enc(buildId) + "/status",
                java.util.Collections.singletonMap("logsOffset", String.valueOf(logsOffset)), null, BuildStatus.class);
    }
    public java.util.concurrent.CompletableFuture<BuildStatus> getBuildStatusAsync(String templateId,String buildId){return AsyncSupport.supplyAsync(()->{try{return getBuildStatus(templateId,buildId);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<BuildStatus> getBuildStatusAsync(String templateId, String buildId, int logsOffset) {
        return AsyncSupport.supplyAsync(() -> { try { return getBuildStatus(templateId, buildId, logsOffset); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    /**
     * Checks an alias, matching JS/Python {@code aliasExists}: 404 means the
     * alias is free, 403 means it exists but is owned by someone else.
     */
    public boolean exists(String alias) throws SandboxException {
        require(alias);
        rejectLegacyTemplateApi("Template.exists");
        try { getByAlias(alias); return true; }
        catch (NotFoundException e) { return false; }
        catch (ForbiddenException e) { return true; }
    }
    /** Deprecated JS/Python-compatible spelling of {@link #exists(String)}. */
    @Deprecated
    public boolean aliasExists(String alias) throws SandboxException { return exists(alias); }
    /** Assigns tags, returning the API's {@code {buildID, tags}} response. */
    public TemplateTagInfo assignTags(String target, java.util.List<String> tags) throws SandboxException {
        rejectLegacyTemplateApi("Template.assignTags");
        java.util.Map<String,Object> p=new java.util.LinkedHashMap<>();p.put("target",target);p.put("tags",tags);
        return client.request("POST","/templates/tags",p,TemplateTagInfo.class);
    }
    public void removeTags(String name, java.util.List<String> tags) throws SandboxException {
        rejectLegacyTemplateApi("Template.removeTags");
        java.util.Map<String,Object> p=new java.util.LinkedHashMap<>();p.put("name",name);p.put("tags",tags);client.request("DELETE","/templates/tags",p,Object.class);
    }
    public TemplateTag[] getTags(String id) throws SandboxException {
        require(id);
        rejectLegacyTemplateApi("Template.getTags");
        return client.request("GET","/templates/"+enc(id)+"/tags",null,TemplateTag[].class);
    }
    private TemplateInfo getByAlias(String alias) throws SandboxException { return client.request("GET", "/templates/aliases/" + enc(alias), null, TemplateInfo.class); }
    /** Template operations are only supported on modern clusters, matching JS/Python. */
    private void rejectLegacyTemplateApi(String feature) throws SandboxException {
        if (client.isLegacyApiDomain()) throw new BuildException(feature + " is not supported on legacy domains");
    }
    private static String enc(String value){return java.net.URLEncoder.encode(value,java.nio.charset.StandardCharsets.UTF_8).replace("+","%20");}
    private static void validateCopySource(String source) throws BuildException {
        if (source == null || source.isEmpty() || Paths.get(source).isAbsolute())
            throw new BuildException("COPY source must be a relative path");
        String normalized=source.replace('\\','/');
        for (String part : normalized.split("/")) if ("..".equals(part)) throw new BuildException("COPY source must remain within file context");
    }
    private static void require(String value) throws SandboxException { if (value == null || value.isEmpty()) throw new SandboxException("template identifier is required"); }
}
