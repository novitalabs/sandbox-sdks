package ai.novita.sandbox.core;

import java.util.Map;
import com.google.gson.annotations.SerializedName;

/**
 * Information about a sandbox instance.
 */
public class SandboxInfo {
    @SerializedName(value = "sandboxID", alternate = {"sandbox_id", "id"})
    private String id;
    @SerializedName(value = "templateID", alternate = {"template", "template_id"})
    private String template;
    private String state;
    @SerializedName(value = "createdAt", alternate = {"created_at"})
    private String createdAt;
    private Map<String, String> metadata;
    @SerializedName(value = "domain", alternate = {"sandboxDomain", "sandbox_domain"})
    private String domain;
    @SerializedName(value = "envdAccessToken", alternate = {"envd_access_token", "envdToken", "envd_token"})
    private String envdAccessToken;
    @SerializedName(value = "envdVersion", alternate = {"envd_version"})
    private String envdVersion;
    @SerializedName(value = "clientID", alternate = {"clientId", "client_id"})
    private String clientID;
    @SerializedName(value = "trafficAccessToken", alternate = {"traffic_access_token", "trafficToken"})
    private String trafficAccessToken;
    /** Listed-sandbox fields (GET /v2/sandboxes), null on create/connect responses. */
    @SerializedName(value = "alias")
    private String alias;
    @SerializedName(value = "startedAt", alternate = {"started_at"})
    private String startedAt;
    @SerializedName(value = "endAt", alternate = {"end_at"})
    private String endAt;
    @SerializedName(value = "cpuCount", alternate = {"cpu_count"})
    private Integer cpuCount;
    @SerializedName(value = "memoryMB", alternate = {"memory_mb", "memoryMb"})
    private Integer memoryMB;
    @SerializedName(value = "diskSizeMB", alternate = {"disk_size_mb", "diskSizeMb"})
    private Integer diskSizeMB;
    @SerializedName(value = "volumeMounts", alternate = {"volume_mounts"})
    private java.util.List<java.util.Map<String, String>> volumeMounts;
    public String getDomain(){return domain;} public String getEnvdAccessToken(){return envdAccessToken;}
    public String getEnvdVersion(){return envdVersion;}
    public void setDomain(String value){this.domain=value;}
    public String getTrafficAccessToken(){return trafficAccessToken;}
    public void setTrafficAccessToken(String value){this.trafficAccessToken=value;}
    public String getClientID(){return clientID;}
    public void setClientID(String value){this.clientID=value;}
    public void setEnvdAccessToken(String value){this.envdAccessToken=value;}
    public void setEnvdVersion(String value){this.envdVersion=value;}
    public String getAlias(){return alias;}
    public String getStartedAt(){return startedAt;}
    public String getEndAt(){return endAt;}
    public Integer getCpuCount(){return cpuCount;}
    public Integer getMemoryMB(){return memoryMB;}
    public Integer getDiskSizeMB(){return diskSizeMB;}
    public java.util.List<java.util.Map<String, String>> getVolumeMounts(){return volumeMounts;}

    public String getId() {
        return id;
    }

    public void setId(String id) {
        this.id = id;
    }

    public String getTemplate() {
        return template;
    }

    public void setTemplate(String template) {
        this.template = template;
    }

    public String getState() {
        return state;
    }

    public void setState(String state) {
        this.state = state;
    }

    public String getCreatedAt() {
        return createdAt;
    }

    public void setCreatedAt(String createdAt) {
        this.createdAt = createdAt;
    }

    public Map<String, String> getMetadata() {
        return metadata;
    }

    public void setMetadata(Map<String, String> metadata) {
        this.metadata = metadata;
    }
}
