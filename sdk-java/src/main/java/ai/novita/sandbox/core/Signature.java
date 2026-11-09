package ai.novita.sandbox.core;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Base64;

/**
 * v1 signatures for sandbox file URLs.
 * The signature fields and URL encoding follow the sandbox file API contract.
 */
final class Signature {
    private Signature() {}

    /**
     * Computes the signature for a file-system operation.
     *
     * @param path file path inside the sandbox
     * @param operation {@code "read"} for download URLs, {@code "write"} for upload URLs
     * @param user sandbox user; {@code null} means the default user and is
     *             signed as an empty string
     * @param envdAccessToken the sandbox envd access token
     * @param expirationInSeconds optional expiration in seconds from now
     * @return the {@code v1_...} signature and its unix expiration, if any
     */
    static Result get(String path, String operation, String user, String envdAccessToken, Integer expirationInSeconds) {
        if (envdAccessToken == null || envdAccessToken.isEmpty()) {
            throw new IllegalArgumentException("Access token is not set and signature cannot be generated!");
        }
        Long expiration = expirationInSeconds != null
                ? System.currentTimeMillis() / 1000L + expirationInSeconds
                : null;
        if (user == null) {
            user = "";
        }
        String raw = expiration == null
                ? path + ":" + operation + ":" + user + ":" + envdAccessToken
                : path + ":" + operation + ":" + user + ":" + envdAccessToken + ":" + expiration;
        MessageDigest digest;
        try {
            digest = MessageDigest.getInstance("SHA-256");
        } catch (java.security.NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 is unavailable", e);
        }
        byte[] hashed = digest.digest(raw.getBytes(StandardCharsets.UTF_8));
        String encoded = Base64.getEncoder().encodeToString(hashed).replaceAll("=+$", "");
        return new Result("v1_" + encoded, expiration);
    }

    /** Signature value plus the unix timestamp it expires at, if any. */
    static final class Result {
        final String signature;
        final Long expiration;

        Result(String signature, Long expiration) {
            this.signature = signature;
            this.expiration = expiration;
        }
    }
}
