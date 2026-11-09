package ai.novita.sandbox.core;

import java.util.*;
import java.nio.file.*;
import java.io.IOException;

/** Fluent, serializable description of a template build. */
public final class TemplateBuilder {
    private final Map<String,Object> spec = new LinkedHashMap<>();
    private final List<Map<String,Object>> instructions = new ArrayList<>();
    private final List<TemplateEnvironment.Group> envDeclarations = new ArrayList<>();
    private String fileContextPath = ".";
    private boolean inheritImageConfig;
    private TemplateImageConfig imageConfig;
    private ImageConfigFailurePolicy configFailurePolicy = ImageConfigFailurePolicy.WARN;
    private okhttp3.OkHttpClient registryHttpClient;
    /** Configures failure handling for registry reads; diagnostics are retained in every mode. */
    public TemplateBuilder onConfigFailure(ImageConfigFailurePolicy policy) {
        if(policy==null)throw new IllegalArgumentException("image configuration failure policy is required");
        configFailurePolicy=policy; return this;
    }
    /** Uses a caller-owned registry client, for example with a private CA or proxy. */
    public TemplateBuilder imageConfigHttpClient(okhttp3.OkHttpClient client) { registryHttpClient=client; imageConfig=null; return this; }

    /** Opt out of registry config reads, for example for offline builds. */
    public TemplateBuilder inheritImageConfig(boolean enabled) { inheritImageConfig = enabled; if (!enabled) imageConfig = null; return this; }
    /** Diagnostic from the last image config read; null means no failure or warning. */
    public Map<String,String> getImageConfigFailure() {
        if (imageConfig == null) return null;
        Map<String,String> result = new LinkedHashMap<>();
        if (imageConfig.error != null) result.put("error", imageConfig.error);
        if (imageConfig.unusable != null) result.put("unusable", imageConfig.unusable);
        if (imageConfig.unsupportedBase != null) result.put("unsupportedBase", imageConfig.unsupportedBase);
        return result.isEmpty() ? null : Collections.unmodifiableMap(result);
    }
    void resolveInheritedConfig(java.util.function.Consumer<BuildLogEntry> logs) {
        if (!inheritImageConfig || !spec.containsKey("fromImage")) return;
        if(imageConfig==null || imageConfig.error!=null || !String.valueOf(spec.get("fromImage")).contains("@")) {
            String username = null, password = null;
            Object raw = spec.get("fromImageRegistry");
            if (raw instanceof Map) {
                Map<?,?> credentials = (Map<?,?>)raw;
                if ("registry".equals(credentials.get("type"))) {
                    username = (String)credentials.get("username"); password = (String)credentials.get("password");
                } else if ("gcp".equals(credentials.get("type"))) {
                    username = "_json_key"; password = (String)credentials.get("serviceAccountJson");
                }
            }
            imageConfig = TemplateImageConfig.fetch(String.valueOf(spec.get("fromImage")), username, password, registryHttpClient);
        }
        if(imageConfig.error!=null){
            String message=imageConfig.error+". Building without the image's environment, working directory and start command.";
            if(configFailurePolicy==ImageConfigFailurePolicy.RAISE)throw new ImageConfigException(imageConfig.error);
            if(configFailurePolicy==ImageConfigFailurePolicy.WARN){System.err.println(message);if(logs!=null)logs.accept(new BuildLogEntry("warn",message));}
        }
        if (logs != null && imageConfig.unsupportedBase != null) logs.accept(new BuildLogEntry("warn",
                "Image base may not support sandbox provisioning: " + imageConfig.unsupportedBase));
    }
    /**
     * Sticky flag set by {@link #skipCache()}. It is intentionally never cleared:
     * every subsequent instruction carries {@code force}, and a later
     * {@code fromImage}/{@code fromTemplate} invalidates the whole template.
     * This mirrors the JS and Python SDKs.
     */
    private boolean forceNextLayer;
    public static TemplateBuilder create() { return new TemplateBuilder(); }
    /** Sets the base directory used to resolve relative COPY sources and .dockerignore. */
    public TemplateBuilder fileContextPath(String path) { if (path == null || path.trim().isEmpty()) throw new IllegalArgumentException("fileContextPath is required"); fileContextPath = path; return this; }
    public String getFileContextPath() { return fileContextPath; }
    public TemplateBuilder fromImage(String image) { return fromImage(image, true); }
    public TemplateBuilder fromImage(String image, boolean inheritConfig) {
        if (image == null || image.trim().isEmpty()) throw new IllegalArgumentException("image is required");
        spec.put("fromImage", image); spec.remove("fromTemplate"); spec.remove("fromImageRegistry");
        imageConfig = null; inheritImageConfig = inheritConfig;
        if (forceNextLayer) spec.put("force", true); return this;
    }
    public TemplateBuilder fromImage(String image, String username, String password) { return fromImage(image, username, password, true); }
    public TemplateBuilder fromImage(String image, String username, String password, boolean inheritConfig) {
        if (username == null || password == null) throw new IllegalArgumentException("registry username and password are required");
        fromImage(image, inheritConfig);
        spec.put("fromImageRegistry", map("type", "registry", "username", username, "password", password)); return this;
    }
    public TemplateBuilder fromDebianImage(String v) { return fromImage("debian:" + (v == null || v.isEmpty() ? "stable" : v)); }
    public TemplateBuilder fromUbuntuImage(String v) { return fromImage("ubuntu:" + (v == null || v.isEmpty() ? "latest" : v)); }
    public TemplateBuilder fromPythonImage(String v) { return fromImage("python:" + (v == null || v.isEmpty() ? "3" : v)); }
    public TemplateBuilder fromNodeImage(String v) { return fromImage("node:" + (v == null || v.isEmpty() ? "lts" : v)); }
    public TemplateBuilder fromBunImage(String v) { return fromImage("oven/bun:" + (v == null || v.isEmpty() ? "latest" : v)); }
    public TemplateBuilder fromTemplate(String id) { spec.remove("fromImage"); spec.remove("fromImageRegistry"); inheritImageConfig = false; imageConfig = null; spec.put("fromTemplate", id); if (forceNextLayer) spec.put("force", true); return this; }
    public TemplateBuilder fromBaseImage() { return fromImage("novita/base:latest"); }
    public TemplateBuilder fromBaseImage(String image) { return fromImage(image == null || image.isEmpty() ? "novita/base:latest" : image); }

    /**
     * Parses a Dockerfile, either as raw content or as a path to a file.
     *
     * <p>Supported instructions: {@code FROM}, {@code RUN}, {@code COPY},
     * {@code ADD}, {@code WORKDIR}, {@code USER}, {@code ENV}, {@code ARG},
     * {@code CMD} and {@code ENTRYPOINT}. {@code EXPOSE} and {@code VOLUME} are
     * ignored, matching the JS/Python SDKs. Multi-stage Dockerfiles are rejected.
     *
     * @param dockerfileContentOrPath Dockerfile text, or a path to an existing file
     * @throws SandboxException if the input has no {@code FROM} or more than one
     */
    public TemplateBuilder fromDockerfile(String dockerfileContentOrPath) throws SandboxException {
        String content = readDockerfile(dockerfileContentOrPath);
        List<String> lines = logicalLines(content);

        List<String> froms = new ArrayList<>();
        for (String line : lines) {
            if (keyword(line).equals("FROM")) froms.add(argument(line));
        }
        if (froms.isEmpty()) throw new BuildException("Dockerfile must contain a FROM instruction");
        if (froms.size() > 1) throw new BuildException("Multi-stage Dockerfiles are not supported");
        fromImage(froms.get(0));

        // Docker defaults, overridden below if the Dockerfile sets them.
        user("root");
        workdir("/");
        boolean userChanged = false;
        boolean workdirChanged = false;
        String entrypoint = null, defaultCommand = null;

        for (String line : lines) {
            String kw = keyword(line);
            String rest = argument(line);
            switch (kw) {
                case "FROM":
                    break;
                case "RUN":
                    if (!rest.isEmpty()) run(rest);
                    break;
                case "COPY":
                case "ADD": {
                    parseCopy(rest);
                    break;
                }
                case "WORKDIR":
                    if (!rest.isEmpty()) { workdir(rest); workdirChanged = true; }
                    break;
                case "USER":
                    if (!rest.isEmpty()) { user(rest); userChanged = true; }
                    break;
                case "ENV":
                case "ARG":
                    setEnvs(parseEnv(rest, kw), "ENV".equals(kw));
                    break;
                case "CMD":
                    defaultCommand = rest;
                    break;
                case "ENTRYPOINT":
                    entrypoint = rest;
                    break;
                case "EXPOSE":
                case "VOLUME":
                    break;
                default:
                    // Unknown directives are ignored rather than failing the build.
                    break;
            }
        }

        if (entrypoint != null || defaultCommand != null) {
            String startup = dockerfileStartCommand(entrypoint, defaultCommand);
            command(startup); spec.putIfAbsent("readyCmd", "sleep 20");
        }
        if (!userChanged) user("user");
        if (!workdirChanged) workdir("/home/user");
        return this;
    }

    public TemplateBuilder fromAwsRegistry(String image,String accessKeyId,String secretAccessKey,String region) { fromImage(image); return registry("aws", "awsAccessKeyId",accessKeyId,"awsSecretAccessKey",secretAccessKey,"awsRegion",region); }
    public TemplateBuilder fromAWSRegistry(String image,String accessKeyId,String secretAccessKey,String region) { return fromAwsRegistry(image, accessKeyId, secretAccessKey, region); }
    public TemplateBuilder fromGcpRegistry(String image,String serviceAccountJson) { fromImage(image); spec.put("fromImageRegistry", map("type","gcp","serviceAccountJson",serviceAccountJson)); return this; }
    public TemplateBuilder fromGCPRegistry(String image,String serviceAccountJson) { return fromGcpRegistry(image, serviceAccountJson); }
    public TemplateBuilder fromOciRegistry(String image,String tenancyOcid,String userOcid,String fingerprint,String privateKey,String region) { fromImage(image); spec.put("fromImageRegistry", map("type","oci","tenancyOcid",tenancyOcid,"userOcid",userOcid,"fingerprint",fingerprint,"privateKey",privateKey,"region",region)); return this; }
    public TemplateBuilder fromOCIRegistry(String image,String tenancyOcid,String userOcid,String fingerprint,String privateKey,String region) { return fromOciRegistry(image, tenancyOcid, userOcid, fingerprint, privateKey, region); }
    public TemplateBuilder fromHuaweiCloudRegistry(String image,String accessKeyId,String secretAccessKey,String region) { fromImage(image); return registry("huawei", "accessKeyId",accessKeyId,"secretAccessKey",secretAccessKey,"region",region); }
    /** Adds a build-only variable; use the boolean overload to persist it at runtime. */
    public TemplateBuilder environment(String key,String value) { return environment(key,value,false); }
    public TemplateBuilder environment(String key,String value,boolean runtime) { return setEnvs(Collections.singletonMap(key,value),runtime); }
    public TemplateBuilder environments(Map<String,String> values) { return setEnvs(values,false); }
    public TemplateBuilder environments(Map<String,String> values,boolean runtime) { return setEnvs(values,runtime); }
    public TemplateBuilder setEnvs(Map<String,String> values) { return setEnvs(values,false); }
    /** One declaration group resolves expressions against the preceding environment. */
    public TemplateBuilder setEnvs(Map<String,String> values,boolean runtime) {
        if(values.isEmpty())return this;
        List<String> args=new ArrayList<>();values.forEach((key,value)->{args.add(key);args.add(value);});
        instruction("env",args.toArray(new String[0]));envDeclarations.add(new TemplateEnvironment.Group(values,runtime));return this;
    }
    /**
     * Sets the command started when a sandbox boots.
     *
     * <p>Only {@code startCmd} is set: the build protocol has no {@code CMD}
     * instruction type, matching the JS/Python SDKs.
     */
    public TemplateBuilder command(String command) { spec.put("startCmd", command); return this; }
    public TemplateBuilder setStartCmd(String command) { return command(command); }
    public TemplateBuilder run(String command) { return run(command, null); }
    public TemplateBuilder runCmd(String command) { return run(command); }

    /**
     * Runs a shell command during the build.
     *
     * @param command the command line
     * @param user optional user to run as; encoded as the second element of the
     *             instruction args, matching the JS/Python SDKs
     */
    public TemplateBuilder run(String command, String user) {
        if (user == null || user.isEmpty()) instruction("run", command);
        else instruction("run", command, user);
        return this;
    }

    /**
     * Installs system packages with apt-get.
     *
     * <p>Runs {@code apt-get update} first, otherwise the install fails on a
     * clean Debian image because the package index is missing. Matches the
     * JS/Python SDKs, which also run the two commands as one root shell.
     */
    public TemplateBuilder installPackages(String... packages) {
        if (packages == null || packages.length == 0) return this;
        String install = "DEBIAN_FRONTEND=noninteractive DEBCONF_NOWARNINGS=yes apt-get install -y " + joinQuoted(packages);
        return run("apt-get update && " + install, "root");
    }
    public TemplateBuilder aptInstall(String... packages) { return installPackages(packages); }
    public TemplateBuilder exposePorts(int... ports) { List<Integer> p=new ArrayList<>(); for(int x:ports)p.add(x); spec.put("ports",p); return this; }
    public TemplateBuilder ports(int... ports) { return exposePorts(ports); }
    public TemplateBuilder workdir(String path) { instruction("workdir",path); return this; }
    public TemplateBuilder setWorkdir(String path) { return workdir(path); }
    public TemplateBuilder user(String user) { instruction("user",user); return this; }
    public TemplateBuilder setUser(String user) { return user(user); }
    public TemplateBuilder patchCommand(String command) { spec.put("preBootScript",command); return this; }
    public TemplateBuilder setPatchCmd(String command) { return patchCommand(command); }
    public TemplateBuilder readyCommand(String command) { spec.put("readyCmd",command); return this; }
    public TemplateBuilder setReadyCmd(String command) { return readyCommand(command); }
    /**
     * Writes a file into the image during the build.
     *
     * <p>The build protocol has no {@code WRITE} instruction type, so this is
     * implemented as a root {@code RUN} that base64-decodes the content. The
     * encoding keeps arbitrary bytes (quotes, newlines, {@code $}) intact.
     */
    public TemplateBuilder writeFile(String path,String content) { return writeFile(path,content,false); }
    public TemplateBuilder writeFile(String path,String content,boolean forceUpload) {
        String encoded = java.util.Base64.getEncoder().encodeToString(
                (content == null ? "" : content).getBytes(java.nio.charset.StandardCharsets.UTF_8));
        return run("printf %s " + Shell.quote(encoded) + " | base64 -d > " + Shell.quote(path), "root");
    }
    public TemplateBuilder writeText(String path,String content) { return writeFile(path,content); }
    public TemplateBuilder copyFile(String source,String destination) { return copyFile(source,destination,null,null,false,null); }
    public TemplateBuilder copyFile(String source,String destination,boolean forceUpload) { return copyFile(source,destination,null,null,forceUpload,null); }
    public TemplateBuilder copyFile(String source,String destination,String user,Integer mode,boolean forceUpload,Boolean resolveSymlinks) {
        List<String> matches = expandGlob(source);
        if (matches.isEmpty()) matches = Collections.singletonList(source);
        for (String match : matches) {
            Map<String,Object> step=new LinkedHashMap<>(); step.put("type","COPY"); step.put("args",Arrays.asList(match,destination,user==null?"":user,mode==null?"":padOctal(mode))); step.put("force",forceUpload||forceNextLayer); if(forceUpload) step.put("forceUpload",true); if(resolveSymlinks!=null) step.put("resolveSymlinks",resolveSymlinks); instructions.add(step);
        }
        return this;
    }
    public TemplateBuilder copy(String source,String destination) { return copyFile(source,destination); }
    /** Adds one COPY step for each source, matching the JS array-source overload. */
    public TemplateBuilder copy(List<String> sources, String destination) {
        if (sources != null) for (String source : sources) copyFile(source, destination);
        return this;
    }
    public TemplateBuilder copyItems(List<Map<String,Object>> items) {
        if (items == null) return this;
        for (Map<String,Object> i : items) {
            if (i == null) continue;
            String src = String.valueOf(i.get("src"));
            String dest = String.valueOf(i.get("dest"));
            boolean force = Boolean.TRUE.equals(i.get("forceUpload"));
            String user = i.get("user") == null ? "" : String.valueOf(i.get("user"));
            Integer mode = i.get("mode") instanceof Number ? ((Number)i.get("mode")).intValue() : null;
            Boolean resolve = i.get("resolveSymlinks") instanceof Boolean ? (Boolean)i.get("resolveSymlinks") : null;
            copyFile(src, dest, user, mode, force, resolve);
        }
        return this;
    }
    public TemplateBuilder remove(String path) { return remove(path,false,false,null); }
    public TemplateBuilder remove(String path, boolean recursive, boolean force, String user) { return run((recursive?"rm -r ":"rm ")+(force?"-f ":"")+Shell.quote(path), user); }
    public TemplateBuilder rename(String source,String destination,boolean force,String user) { return run("mv "+(force?"-f ":"")+Shell.quote(source)+" "+Shell.quote(destination), user); }
    public TemplateBuilder rename(String source,String destination) { return rename(source,destination,false,null); }
    public TemplateBuilder makeDir(String path,boolean parents,Integer mode,String user) { return run("mkdir "+(parents?"-p ":"")+Shell.quote(path)+(mode==null?"":" && chmod "+padOctal(mode)+" "+Shell.quote(path)), user); }
    public TemplateBuilder makeDir(String path) { return makeDir(path,true,null,null); }
    public TemplateBuilder makeSymlink(String target,String link,String user) { return run("ln -s "+Shell.quote(target)+" "+Shell.quote(link), user); }
    public TemplateBuilder makeSymlink(String target,String link) { return makeSymlink(target,link,null); }
    public TemplateBuilder pipInstall(String... packages) { return run("pip install "+(packages.length==0?".":joinQuoted(packages))); }
    public TemplateBuilder pipInstall(List<String> packages) { return pipInstall(packages.toArray(new String[0])); }
    public TemplateBuilder npmInstall(String... packages) { return run("npm install "+joinQuoted(packages)); }
    public TemplateBuilder npmInstall(List<String> packages) { return npmInstall(packages.toArray(new String[0])); }
    public TemplateBuilder bunInstall(String... packages) { return run("bun install "+joinQuoted(packages)); }
    public TemplateBuilder bunInstall(List<String> packages) { return bunInstall(packages.toArray(new String[0])); }
    public TemplateBuilder addMcpServer(String... servers) { return run("mcp-gateway install "+joinQuoted(servers)); }
    public TemplateBuilder gitClone(String url,String path) { return run("git clone "+Shell.quote(url)+" "+Shell.quote(path)); }
    public TemplateBuilder betaDevContainerPrebuild(String directory) { return run("devcontainer build --workspace-folder "+Shell.quote(directory)); }
    public TemplateBuilder betaSetDevContainerStart(String directory) { return command("sudo devcontainer up --workspace-folder "+Shell.quote(directory)); }
    public TemplateBuilder skipCache() { forceNextLayer=true; return this; }
    public TemplateBuilder with(String key,Object value) { spec.put(key,value); if ("fromImageRegistry".equals(key)) imageConfig = null; return this; }
    Map<String,Object> toMap() {
        Map<String,Object> out=new LinkedHashMap<>(spec);
        List<Map<String,Object>> steps = new ArrayList<>(instructions);
        Map<String,String> effectiveEnvs = new LinkedHashMap<>();
        if (imageConfig != null && imageConfig.error == null) {
            effectiveEnvs.putAll(imageConfig.env);
            if (imageConfig.workdir != null && !imageConfig.workdir.isEmpty())
                steps.add(0,map("type", "WORKDIR", "args", Collections.singletonList(imageConfig.workdir), "force", false));
            String command = TemplateImageConfig.effectiveCommand(imageConfig.entrypoint, imageConfig.cmd);
            if (command != null && !out.containsKey("startCmd")) {
                out.put("startCmd", command); out.putIfAbsent("readyCmd", "sleep 20");
            }
        }
        String runtime=TemplateEnvironment.command(effectiveEnvs,envDeclarations);
        if(runtime!=null){
            String snapshot=TemplateEnvironment.snapshot(envDeclarations);
            if(snapshot!=null)steps.add(0,map("type","RUN","args",Arrays.asList(snapshot,"root"),"force",false));
            steps.add(map("type","RUN","args",Arrays.asList(runtime,"root"),"force",forceNextLayer));
        }
        // JS/Python serialize() always emit the steps array and force flag,
        // empty and false included.
        out.put("steps", steps);
        out.put("force", forceNextLayer);
        return out;
    }
    public String toJson() { resolveInheritedConfig(null); return new com.google.gson.Gson().toJson(toMap()); }
    /**
     * Renders the builder back to a Dockerfile.
     *
     * <p>Mirrors the JS/Python SDKs: {@code RUN}/{@code COPY}/{@code ENV} get
     * their Dockerfile spelling, other instructions are emitted verbatim, and
     * {@code startCmd} becomes {@code ENTRYPOINT}.
     *
     * @throws SandboxException if the template has no base image, or is based on
     *         another template (which cannot be expressed as a Dockerfile)
     */
    public String toDockerfile() throws SandboxException {
        resolveInheritedConfig(null);
        Map<String,Object> resolved = toMap();
        if (spec.get("fromTemplate") != null) throw new BuildException("Cannot convert a template built from another template to a Dockerfile; such templates can only be built through the API");
        Object image = spec.get("fromImage");
        if (image == null) throw new BuildException("No base image specified for template");
        StringBuilder s = new StringBuilder("FROM ").append(image).append('\n');
        for (Map<String,Object> i : (List<Map<String,Object>>)resolved.get("steps")) {
            String t = String.valueOf(i.get("type"));
            List<?> a = (List<?>) i.get("args");
            if ("RUN".equals(t)) s.append("RUN ").append(a.get(0)).append('\n');
            else if ("COPY".equals(t)) {
                s.append("COPY ");
                if (a.size() > 2 && !String.valueOf(a.get(2)).isEmpty()) s.append("--chown=").append(a.get(2)).append(' ');
                if (a.size() > 3 && !String.valueOf(a.get(3)).isEmpty()) s.append("--chmod=").append(a.get(3)).append(' ');
                s.append(new com.google.gson.Gson().toJson(Arrays.asList(a.get(0), a.get(1)))).append('\n');
            }
            else if ("ENV".equals(t)) {
                StringBuilder v = new StringBuilder();
                for (int k = 0; k + 1 < a.size(); k += 2) { if (v.length() > 0) v.append(' '); v.append(a.get(k)).append('=').append(a.get(k + 1)); }
                s.append("ENV ").append(v).append('\n');
            } else s.append(t).append(' ').append(join(a)).append('\n');
        }
        if (resolved.get("startCmd") != null) s.append("ENTRYPOINT ").append(resolved.get("startCmd")).append('\n');
        return s.toString();
    }
    /**
     * Instruction types on the wire are upper case ({@code RUN}, {@code WORKDIR},
     * {@code USER}, {@code ENV}, {@code COPY}) - the same enum the JS and Python
     * SDKs send. Lower-case types are not part of the build protocol.
     */
    private void instruction(String type,String... args) { Map<String,Object> i=new LinkedHashMap<>(); i.put("type",type.toUpperCase(Locale.ROOT)); i.put("args",Arrays.asList(args)); i.put("force",forceNextLayer); instructions.add(i); }
    private void instructionWithForce(String type,boolean force,String... args) { Map<String,Object> i=new LinkedHashMap<>(); i.put("type",type.toUpperCase(Locale.ROOT)); i.put("args",Arrays.asList(args)); if(force||forceNextLayer)i.put("force",true); i.put("forceUpload",force); instructions.add(i); }
    private TemplateBuilder registry(String type,String... values){Map<String,Object> m=new LinkedHashMap<>();m.put("type",type);for(int i=0;i<values.length;i+=2)m.put(values[i],values[i+1]);spec.put("fromImageRegistry",m);return this;}
    private static Map<String,Object> map(Object... v){Map<String,Object> m=new LinkedHashMap<>();for(int i=0;i<v.length;i+=2)m.put(String.valueOf(v[i]),v[i+1]);return m;}
    private static String joinQuoted(String[] values){StringBuilder b=new StringBuilder();for(String v:values){if(b.length()>0)b.append(' ');b.append(Shell.quote(v));}return b.toString();}
    /** Double-quote env values so normal variable references expand at build time while command substitution cannot run. */
    private static String envDoubleQuote(String value) {
        return "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"")
                .replace("`", "\\`").replace("$(", "\\$(") + "\"";
    }
    private static String join(List<?> values){StringBuilder b=new StringBuilder();for(Object v:values){if(b.length()>0)b.append(' ');b.append(v);}return b.toString();}
    private List<String> expandGlob(String source) {
        if (source == null || !(source.contains("*") || source.contains("?") || source.contains("["))) return Collections.singletonList(source);
        List<String> result = new ArrayList<>();
        Path base = Paths.get(fileContextPath).toAbsolutePath().normalize();
        try (java.util.stream.Stream<Path> walk = java.nio.file.Files.walk(base)) {
            java.nio.file.PathMatcher matcher = java.nio.file.FileSystems.getDefault().getPathMatcher("glob:" + source.replace('\\','/'));
            walk.forEach(p -> { if (matcher.matches(base.relativize(p))) result.add(base.relativize(p).toString().replace(java.io.File.separatorChar,'/')); });
        } catch (IOException ignored) { }
        Collections.sort(result);
        return result;
    }

    /** Zero-pads a numeric file mode to four octal digits, e.g. 493 -> "0755". */
    private static String padOctal(int mode) { return String.format("%04o", mode); }

    // ---------------------------------------------------------------- Dockerfile

    /** Reads the Dockerfile from disk when the argument names an existing file. */
    private static String readDockerfile(String contentOrPath) throws SandboxException {
        if (contentOrPath == null) throw new BuildException("dockerfile content is required");
        try {
            java.nio.file.Path p = java.nio.file.Paths.get(contentOrPath);
            if (java.nio.file.Files.isRegularFile(p)) return new String(java.nio.file.Files.readAllBytes(p), java.nio.charset.StandardCharsets.UTF_8);
        } catch (RuntimeException | java.io.IOException ignored) {
            // Not a path (or unreadable) - fall through and treat it as content.
        }
        return contentOrPath;
    }

    /**
     * Splits a Dockerfile into logical lines: comments and blank lines are
     * dropped, and backslash continuations are joined into one line.
     */
    private static List<String> logicalLines(String content) {
        List<String> out = new ArrayList<>();
        StringBuilder current = new StringBuilder();
        for (String raw : content.split("\\r?\\n", -1)) {
            String line = raw.trim();
            if (current.length() == 0 && (line.isEmpty() || line.startsWith("#"))) continue;
            if (line.endsWith("\\")) {
                current.append(line, 0, line.length() - 1).append(' ');
                continue;
            }
            current.append(line);
            String joined = current.toString().trim();
            if (!joined.isEmpty()) out.add(joined);
            current.setLength(0);
        }
        String tail = current.toString().trim();
        if (!tail.isEmpty()) out.add(tail);
        return out;
    }

    private static String keyword(String line) {
        int i = 0;
        while (i < line.length() && !Character.isWhitespace(line.charAt(i))) i++;
        return line.substring(0, i).toUpperCase(Locale.ROOT);
    }

    private static String argument(String line) {
        int i = 0;
        while (i < line.length() && !Character.isWhitespace(line.charAt(i))) i++;
        return line.substring(i).trim();
    }

    /** Preserve COPY options and both shell/JSON source forms. */
    private void parseCopy(String rest) throws BuildException {
        String owner = null;
        Integer mode = null;
        while (rest.startsWith("--")) {
            int space = rest.indexOf(' ');
            if (space < 0) throw new BuildException("COPY requires source and destination");
            String flag = rest.substring(0, space);
            rest = rest.substring(space + 1).trim();
            if (flag.startsWith("--chown=")) owner = flag.substring(8);
            else if (flag.startsWith("--chmod=")) {
                try { mode = Integer.parseInt(flag.substring(8), 8); }
                catch (NumberFormatException e) { throw new BuildException("Invalid COPY chmod", e); }
            } else throw new BuildException("Unsupported COPY option: " + flag);
        }
        List<String> parts = new ArrayList<>();
        if (rest.startsWith("[")) {
            try {
                com.google.gson.JsonArray array = com.google.gson.JsonParser.parseString(rest).getAsJsonArray();
                for (com.google.gson.JsonElement item : array) {
                    if (!item.isJsonPrimitive() || !item.getAsJsonPrimitive().isString()) throw new IllegalArgumentException();
                    parts.add(item.getAsString());
                }
            } catch (RuntimeException e) { throw new BuildException("COPY JSON form must be an array of strings", e); }
        } else {
            StringBuilder word = new StringBuilder(); char quote = 0; boolean escaped = false;
            for (char c : rest.toCharArray()) {
                if (escaped) { word.append(c); escaped = false; }
                else if (c == '\\') escaped = true;
                else if (quote != 0) { if (c == quote) quote = 0; else word.append(c); }
                else if (c == '\'' || c == '"') quote = c;
                else if (Character.isWhitespace(c)) { if (word.length() > 0) { parts.add(word.toString()); word.setLength(0); } }
                else word.append(c);
            }
            if (escaped || quote != 0) throw new BuildException("Unterminated COPY argument");
            if (word.length() > 0) parts.add(word.toString());
        }
        if (parts.size() < 2) throw new BuildException("COPY requires source and destination");
        for (int i = 0; i < parts.size() - 1; i++) copyFile(parts.get(i), parts.get(parts.size() - 1), owner, mode, false, null);
    }

    /** Parses {@code ENV}/{@code ARG} in both {@code k=v} and {@code k v} forms. */
    private static Map<String,String> parseEnv(String rest,String keyword) {
        Map<String,String> values=new LinkedHashMap<>();List<String> words=new ArrayList<>();
        StringBuilder word=new StringBuilder();char quote=0;boolean escaped=false,started=false;
        for(char c:rest.toCharArray()){
            if(escaped){if(quote=='"'&&c!='"'&&c!='\\')word.append('\\');word.append(c);escaped=false;started=true;}
            else if(c=='\\'&&quote!='\''){escaped=true;started=true;}
            else if(quote!=0){if(c==quote)quote=0;else word.append(c);started=true;}
            else if(c=='\''||c=='"'){quote=c;started=true;}
            else if(Character.isWhitespace(c)){if(started){words.add(word.toString());word.setLength(0);started=false;}}
            else{word.append(c);started=true;}
        }
        if(quote!=0||escaped)throw new IllegalArgumentException("Unterminated environment declaration");
        if(started)words.add(word.toString());if(words.isEmpty())return values;
        if("ENV".equals(keyword)&&!words.get(0).contains("=")){values.put(words.get(0),String.join(" ",words.subList(1,words.size())));return values;}
        for(String token:words){int eq=token.indexOf('=');if(eq>0)values.put(token.substring(0,eq),token.substring(eq+1));else if("ARG".equals(keyword))values.put(token,"");}
        return values;
    }

    /** Preserves Docker's shell/exec distinction when combining startup directives. */
    private static String dockerfileStartCommand(String entrypoint, String defaultCommand) {
        // A shell-form ENTRYPOINT runs independently of CMD.
        if (entrypoint != null && !entrypoint.trim().startsWith("[")) return entrypoint;
        String entry = entrypoint == null ? "" : stripJsonArray(entrypoint);
        String cmd = defaultCommand == null ? "" : stripJsonArray(defaultCommand);
        if (!entry.isEmpty() && defaultCommand != null && !defaultCommand.trim().startsWith("[")) {
            // Docker appends the shell and its script as arguments to an exec ENTRYPOINT.
            cmd = TemplateImageConfig.effectiveCommand(Collections.emptyList(),
                    Arrays.asList("/bin/sh", "-c", defaultCommand));
        }
        return (entry + " " + cmd).trim();
    }

    /** Turns a JSON-array CMD/ENTRYPOINT into a plain command line. */
    private static String stripJsonArray(String rest) {
        String s = rest.trim();
        if (!s.startsWith("[")) return s;
        try {
            com.google.gson.JsonElement e = com.google.gson.JsonParser.parseString(s);
            if (e.isJsonArray()) {
                List<String> parts = new ArrayList<>();
                for (com.google.gson.JsonElement item : e.getAsJsonArray()) parts.add(item.getAsString());
                String command = TemplateImageConfig.effectiveCommand(Collections.emptyList(), parts);
                return command == null ? "" : command;
            }
        } catch (RuntimeException ignored) {
            // Not valid JSON - keep the original text.
        }
        return s;
    }
}
