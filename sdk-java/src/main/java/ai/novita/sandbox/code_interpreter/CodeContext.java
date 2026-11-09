package ai.novita.sandbox.code_interpreter;

public class CodeContext {
    @com.google.gson.annotations.SerializedName(value="id",alternate={"context_id"})
    private String id;
    private String language,cwd;
    public String getId(){return id;}
    public String getLanguage(){return language;}
    public String getCwd(){return cwd;}
    public void setId(String value){id=value;}
    public void setLanguage(String value){language=value;}
    public void setCwd(String value){cwd=value;}
}
