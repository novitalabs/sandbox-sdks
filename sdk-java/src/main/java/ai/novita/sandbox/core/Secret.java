package ai.novita.sandbox.core;
import java.util.*;
import java.util.concurrent.*;
import java.util.regex.Pattern;

/** Control-plane secret operations, aligned with the JS/Python Secret API. */
public final class Secret {
    private static final Pattern NAME_PATTERN = Pattern.compile("^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$");
    private static final Pattern HOST_PATTERN = Pattern.compile("^(\\*\\.)?[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$");

    private final ApiClient client; Secret(ApiClient c){client=c;}

    private static String path(String n)throws SecretException{
        validateName(n);
        return java.net.URLEncoder.encode(n,java.nio.charset.StandardCharsets.UTF_8).replace("+","%20");
    }

    /** Matches JS {@code validateSecretName}: 1-128 chars, letters/digits/dot/underscore/hyphen. */
    static void validateName(String name) throws SecretException {
        if (name == null || !NAME_PATTERN.matcher(name).matches()) {
            throw new SecretException("secret name must be 1-128 characters and contain only letters, numbers, dots, underscores, and hyphens");
        }
    }

    /** Matches JS {@code normalizeSecretHosts}: trim+lowercase, hostname-only, wildcards, dedupe. */
    static List<String> normalizeHosts(List<String> hosts) throws SecretException {
        if (hosts == null || hosts.isEmpty()) throw new SecretException("hosts is required");
        Set<String> seen = new LinkedHashSet<>();
        for (String raw : hosts) {
            if (raw == null) throw new SecretException("host must be a string");
            String host = raw.trim().toLowerCase(java.util.Locale.ROOT);
            if (host.isEmpty()) throw new SecretException("host cannot be empty");
            if (host.contains("://") || host.matches(".*[/?#@:].*")) throw new SecretException("host must be a hostname only");
            boolean wildcard = host.startsWith("*.");
            String lookup = wildcard ? host.substring(2) : host;
            if (lookup.isEmpty() || lookup.contains("*")) throw new SecretException("invalid host: " + raw);
            String normalized = wildcard ? "*." + lookup : lookup;
            if (!HOST_PATTERN.matcher(normalized).matches()) throw new SecretException("host must be a hostname or wildcard hostname: " + raw);
            seen.add(normalized);
        }
        return new ArrayList<>(seen);
    }

    /** Wire body shared by create/update, matching JS {@code SecretWriteParams}. */
    private static Map<String,Object> writeBody(SecretOptions o, boolean includeName) throws SecretException {
        if (o == null) throw new SecretException("secret options are required");
        validateName(o.getName());
        if (o.getValue() == null || o.getValue().isEmpty()) throw new SecretException("value is required");
        Map<String,Object> body = new LinkedHashMap<>();
        if (includeName) body.put("name", o.getName());
        body.put("value", o.getValue());
        body.put("hosts", normalizeHosts(o.getHosts()));
        if (o.getDescription() != null) body.put("description", o.getDescription());
        return body;
    }

    private void requireModern() throws SandboxException { if (client.isLegacyApiDomain()) throw new SecretException("Secrets are not supported on legacy domains"); }
    public SecretBinding create(SecretOptions o) throws SandboxException {
        requireModern();
        return client.request("POST","/secrets", writeBody(o, true), SecretBinding.class);
    }

    /** Lists secrets; the API answers with a {@code {"secrets": [...]}} envelope. */
    public SecretBinding[] list() throws SandboxException {
        requireModern();
        SecretListEnvelope envelope = client.request("GET","/secrets",null,SecretListEnvelope.class);
        return envelope == null || envelope.secrets == null ? new SecretBinding[0] : envelope.secrets;
    }

    public SecretBinding get(String name) throws SandboxException {
        requireModern();
        return client.request("GET","/secrets/"+path(name),null,SecretBinding.class);
    }

    /** Updates a secret; the name lives in the URL only, matching JS/Python. */
    public SecretBinding update(SecretOptions o) throws SandboxException {
        if (o == null) throw new SecretException("secret options are required");
        requireModern();
        return client.request("PUT","/secrets/"+path(o.getName()), writeBody(o, false), SecretBinding.class);
    }

    /** Deletes a secret and returns the deleted name, matching JS/Python. */
    public String delete(String name) throws SandboxException {
        requireModern();
        SecretDeleteResponse response = client.request("DELETE","/secrets/"+path(name),null,SecretDeleteResponse.class);
        return response == null || response.deleted == null ? "" : response.deleted;
    }

    /** Response envelope for GET /secrets. */
    static class SecretListEnvelope { SecretBinding[] secrets; }
    static class SecretDeleteResponse { String deleted; }

    public CompletableFuture<SecretBinding[]> listAsync(){return AsyncSupport.supplyAsync(()->{try{return list();}catch(Exception e){throw new CompletionException(e);}});}
    public CompletableFuture<SecretBinding> getAsync(String n){return AsyncSupport.supplyAsync(()->{try{return get(n);}catch(Exception e){throw new CompletionException(e);}});}
    public CompletableFuture<SecretBinding> createAsync(SecretOptions o){return AsyncSupport.supplyAsync(()->{try{return create(o);}catch(Exception e){throw new CompletionException(e);}});}
    public CompletableFuture<SecretBinding> updateAsync(SecretOptions o){return AsyncSupport.supplyAsync(()->{try{return update(o);}catch(Exception e){throw new CompletionException(e);}});}
    public CompletableFuture<String> deleteAsync(String n){return AsyncSupport.supplyAsync(()->{try{return delete(n);}catch(Exception e){throw new CompletionException(e);}});}
}
