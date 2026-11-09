package ai.novita.sandbox.core;

/** Alias is optional for sandbox migration and required for template distribution. Multiple templates require aliasForResource. */
public final class JobCreateOptions {
    private String sourceRegion = null;
    private String templateAlias = null;
    private java.util.function.Function<String,String> aliasForResource = null;
    public String getSourceRegion() { return sourceRegion; }
    public JobCreateOptions setSourceRegion(String value) { sourceRegion=value; return this; }
    public String getTemplateAlias() { return templateAlias; }
    public JobCreateOptions setTemplateAlias(String value) { templateAlias=value; return this; }
    public java.util.function.Function<String,String> getAliasForResource() { return aliasForResource; }
    public JobCreateOptions setAliasForResource(java.util.function.Function<String,String> value) { aliasForResource=value; return this; }
}
