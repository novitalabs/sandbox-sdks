package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.*;
import java.io.IOException;
import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.regex.*;

/** Registry v2 config reader. Failures are diagnostic data; no credentials enter diagnostics. */
final class TemplateImageConfig {
    final Map<String,String> env = new LinkedHashMap<>();
    final List<String> entrypoint = new ArrayList<>(), cmd = new ArrayList<>();
    String workdir, error, unusable, unsupportedBase;
    private static final Set<String> SKIP_ENV = new HashSet<>(Arrays.asList("HOSTNAME", "HOME", "PWD", "OLDPWD", "SHLVL", "_", "TERM"));
    private static final String ACCEPT = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json";
    private static final Set<Integer> RETRY = new HashSet<>(Arrays.asList(429, 500, 502, 503, 504));

    static TemplateImageConfig fetch(String image, String username, String password) {
        return fetch(image,username,password,null);
    }
    static TemplateImageConfig fetch(String image, String username, String password, OkHttpClient customClient) {
        TemplateImageConfig result = new TemplateImageConfig();
        OkHttpClient http = (customClient==null ? new OkHttpClient.Builder().callTimeout(30,TimeUnit.SECONDS) : customClient.newBuilder())
                .followRedirects(false).followSslRedirects(false).build();
        ImageReference reference = null;
        try {
            reference = ImageReference.parse(image);
            Reader reader = new Reader(http, username, password, reference.repository);
            HttpUrl base = reference.base;
            JsonObject manifest = reader.read(base.newBuilder().addPathSegment("manifests").addPathSegment(reference.reference).build());
            if (manifest.has("manifests")) {
                String digest = null;
                List<String> offered = new ArrayList<>();
                for (JsonElement element : manifest.getAsJsonArray("manifests")) {
                    JsonObject entry = element.getAsJsonObject(), platform = object(entry, "platform");
                    String os = string(platform, "os"), arch = string(platform, "architecture");
                    if ("linux".equals(os) && "amd64".equals(arch)) digest = string(entry, "digest");
                    if (os != null && !"unknown".equals(os)) offered.add(os + "/" + arch);
                }
                if (digest == null) throw new Unusable("Image has no linux/amd64 variant (offers " + String.join(", ", offered) + ")");
                manifest = reader.read(base.newBuilder().addPathSegment("manifests").addPathSegment(digest).build());
            }
            String digest = string(object(manifest, "config"), "digest");
            if (digest == null || digest.isEmpty()) throw new IOException("Image manifest names no config blob");
            JsonObject blob = reader.read(base.newBuilder().addPathSegment("blobs").addPathSegment(digest).build());
            JsonObject config = object(blob, "config");
            for (String value : strings(config, "Env")) {
                int eq = value.indexOf('=');
                if (eq > 0 && !SKIP_ENV.contains(value.substring(0, eq))) result.env.put(value.substring(0, eq), value.substring(eq + 1));
            }
            result.workdir = string(config, "WorkingDir");
            result.entrypoint.addAll(strings(config, "Entrypoint")); result.cmd.addAll(strings(config, "Cmd"));
            result.unsupportedBase = unsupportedBase(blob);
        } catch (Unusable e) { result.error = result.unusable = e.getMessage(); }
        catch (RegistryFailure e) {
            result.error = "Image registry returned HTTP " + e.status;
            if (e.status == 404) result.unusable = result.error += ": no such image or tag; check spelling and case";
            else if (e.status == 401 && reference != null && reference.base.host().equals("registry-1.docker.io") && reference.repository.startsWith("library/") && username == null)
                result.unusable = result.error += ": no such official Docker Hub image";
            else if (e.status == 401 || e.status == 403) result.error += ": check image name and registry credentials";
        } catch (InterruptedException e) { Thread.currentThread().interrupt(); result.error = "Image configuration read interrupted"; }
        catch (IOException | RuntimeException e) {
            // Exception messages from URL/HTTP libraries may contain credentials or signed URLs.
            result.error = "Could not read image configuration (" + e.getClass().getSimpleName() + ")";
        } finally { if(customClient==null){http.connectionPool().evictAll(); http.dispatcher().executorService().shutdown();} }
        AsyncSupport.throwIfCancelled();
        return result;
    }

    static String effectiveCommand(List<String> entrypoint, List<String> command) {
        List<String> parts = new ArrayList<>(entrypoint); parts.addAll(command);
        if (parts.isEmpty()) return null;
        List<String> quoted = new ArrayList<>();
        for (String part : parts) quoted.add(part.matches("[A-Za-z0-9_./:@%+=,-]+") ? part : Shell.quote(part));
        return String.join(" ", quoted);
    }

    private static JsonObject object(JsonObject value, String key) {
        JsonElement e = value.get(key); return e != null && e.isJsonObject() ? e.getAsJsonObject() : new JsonObject();
    }
    private static String string(JsonObject value, String key) {
        JsonElement e = value.get(key); return e == null || e.isJsonNull() ? null : e.getAsString();
    }
    private static List<String> strings(JsonObject value, String key) {
        List<String> out = new ArrayList<>(); JsonElement e = value.get(key);
        if (e != null && e.isJsonArray()) for (JsonElement item : e.getAsJsonArray()) out.add(item.getAsString());
        return out;
    }
    private static String unsupportedBase(JsonObject blob) {
        StringBuilder history = new StringBuilder();
        if (blob.has("history")) for (JsonElement item : blob.getAsJsonArray("history")) history.append(string(item.getAsJsonObject(), "created_by")).append('\n');
        String text = history.toString();
        if (Pattern.compile("debian\\.sh|debuerreotype|apt-get|apt install|dpkg", Pattern.CASE_INSENSITIVE).matcher(text).find()) return null;
        String[][] families = {{"alpine-minirootfs|etc/alpine-release|apk add|apk --no-cache", "Alpine (musl)"}, {"^BusyBox ", "BusyBox"}, {"microdnf|dnf install|yum install|rpm -i", "RPM-based (Fedora/RHEL/Rocky)"}, {"pacman -S", "Arch"}, {"zypper|KIWI ", "SUSE"}};
        for (String[] family : families) if (Pattern.compile(family[0], Pattern.CASE_INSENSITIVE | Pattern.MULTILINE).matcher(text).find()) return family[1];
        JsonObject config = object(blob, "config");
        if (object(config, "Labels").keySet().stream().anyMatch(k -> k.startsWith("org.opensuse."))) return "SUSE";
        if (strings(config, "Env").stream().anyMatch(v -> v.matches("DISTTAG=f[0-9]+container"))) return "RPM-based (Fedora/RHEL/Rocky)";
        return null;
    }

    static final class ImageReference {
        final HttpUrl base; final String repository, reference;
        ImageReference(HttpUrl base, String repository, String reference) { this.base = base; this.repository = repository; this.reference = reference; }
        static ImageReference parse(String image) {
            if (image == null || image.trim().isEmpty() || image.contains("://") || image.contains(" ")) throw new IllegalArgumentException("Invalid image reference");
            String rest = image.startsWith("docker.io/") ? image.substring(10) : image;
            String host = "registry-1.docker.io";
            int slash = rest.indexOf('/');
            if (slash > 0) {
                String head = rest.substring(0, slash);
                if (head.contains(".") || head.contains(":") || head.equals("localhost")) { host = head; rest = rest.substring(slash + 1); }
            }
            String repository = rest, reference = "latest";
            int at = rest.indexOf('@'), colon = rest.lastIndexOf(':');
            if (at >= 0) { repository = rest.substring(0, at); reference = rest.substring(at + 1); int tag=repository.lastIndexOf(':'); if(tag>repository.lastIndexOf('/'))repository=repository.substring(0,tag); }
            else if (colon > rest.lastIndexOf('/')) { repository = rest.substring(0, colon); reference = rest.substring(colon + 1); }
            if (host.equals("registry-1.docker.io") && !repository.contains("/")) repository = "library/" + repository;
            if (repository.isEmpty() || reference.isEmpty() || repository.contains("..") || host.contains("@")) throw new IllegalArgumentException("Invalid image reference");
            HttpUrl secure = HttpUrl.get("https://" + host);
            String scheme = isLoopback(secure.host()) ? "http" : "https";
            HttpUrl.Builder base = secure.newBuilder().scheme(scheme).addPathSegment("v2");
            for (String part : repository.split("/")) base.addPathSegment(part);
            return new ImageReference(base.build(), repository, reference);
        }
    }
    private static boolean isLoopback(String host) { return host.equals("localhost") || host.equals("127.0.0.1") || host.equals("::1"); }
    private static boolean sameOrigin(HttpUrl a, HttpUrl b) { return a.scheme().equals(b.scheme()) && a.host().equals(b.host()) && a.port() == b.port(); }
    /** A challenge may delegate token minting, but must not export a registry password. */
    static String tokenCredentials(HttpUrl registry, HttpUrl endpoint, String credentials) {
        if (sameOrigin(registry, endpoint)) return credentials;
        if (registry.isHttps() && registry.host().equals("registry-1.docker.io") && registry.port() == 443
                && endpoint.isHttps() && endpoint.host().equals("auth.docker.io") && endpoint.port() == 443) return credentials;
        return null;
    }
    private static final class RegistryFailure extends IOException {
        final int status; final String retryAfter;
        RegistryFailure(int status, String retryAfter) { this.status = status; this.retryAfter = retryAfter; }
    }
    private static final class Unusable extends IOException { Unusable(String message) { super(message); } }

    private static final class Reader {
        final OkHttpClient http; final String basic; final String repository;
        String authorization;
        Reader(OkHttpClient http, String user, String password, String repository) {
            this.repository=repository;
            this.http = http; basic = user == null ? null : Credentials.basic(user, password, java.nio.charset.StandardCharsets.UTF_8); authorization = basic;
        }
        JsonObject read(HttpUrl url) throws IOException, InterruptedException {
            IOException last = null;
            for (int attempt = 0; attempt < 3; attempt++) {
                AsyncSupport.throwIfCancelled();
                if (attempt > 0) {
                    long delay = 1000L << (attempt - 1);
                    if (last instanceof RegistryFailure) try { delay = Math.min(10000, Math.max(0, Long.parseLong(((RegistryFailure)last).retryAfter) * 1000)); } catch (RuntimeException ignored) { }
                    Thread.sleep(delay);
                }
                try { return get(url, authorization, true, 0); }
                catch (RegistryFailure e) { if (!RETRY.contains(e.status)) throw e; last = e; }
                catch (IOException e) { last = e; }
            }
            throw last;
        }
        JsonObject get(HttpUrl url, String auth, boolean challenge, int redirects) throws IOException, InterruptedException {
            Request.Builder request = new Request.Builder().url(url).header("Accept", ACCEPT);
            if (auth != null) request.header("Authorization", auth);
            try (Response response = AsyncSupport.execute(http.newCall(request.build()))) {
                if (response.isRedirect()) {
                    HttpUrl next = url.resolve(response.header("Location", ""));
                    if (next == null || redirects >= 5 || (!next.isHttps() && !(isLoopback(url.host()) && sameOrigin(url, next)))) throw new IOException("Unsafe registry redirect");
                    return get(next, sameOrigin(url, next) ? auth : null, false, redirects + 1);
                }
                if (response.code() == 401 && challenge) {
                    Map<String,String> parameters=null;
                    for(Challenge candidate:response.challenges())if("Bearer".equalsIgnoreCase(candidate.scheme())){parameters=candidate.authParams();break;}
                    if(parameters!=null){
                        String realm = parameters.get("realm");
                        HttpUrl endpoint = realm == null ? null : HttpUrl.parse(realm);
                        if (endpoint == null || (!endpoint.isHttps() && !(isLoopback(url.host()) && sameOrigin(url, endpoint)))) throw new IOException("Unsafe registry token endpoint");
                        HttpUrl.Builder tokenUrl = endpoint.newBuilder();
                        if(parameters.containsKey("service"))tokenUrl.addQueryParameter("service",parameters.get("service"));
                        String scope=parameters.get("scope");if(scope==null||scope.isEmpty())scope="repository:"+repository+":pull";
                        tokenUrl.addQueryParameter("scope",scope);
                        JsonObject token = get(tokenUrl.build(), tokenCredentials(url, endpoint, basic), false, 0);
                        String bearer = string(token, "token"); if (bearer == null) bearer = string(token, "access_token");
                        if (bearer == null || bearer.isEmpty()) throw new IOException("Registry token response has no token");
                        authorization = "Bearer " + bearer;
                        return get(url, authorization, false, 0);
                    }
                }
                if (!response.isSuccessful()) throw new RegistryFailure(response.code(), response.header("Retry-After"));
                if (response.body() == null) throw new IOException("Empty registry response");
                return JsonParser.parseString(response.body().string()).getAsJsonObject();
            }
        }
    }
}
