package ai.novita.sandbox.code_interpreter;

import java.util.*;

/** A rich display or main result. Unknown formats are preserved in getRaw(). */
public final class CodeResult {
    private final Map<String,Object> raw;
    private final boolean mainResult;
    CodeResult(Map<String,Object> value, boolean main){raw=Chart.immutableMap(value);mainResult=main;}
    public boolean isMainResult(){return mainResult;}
    public Map<String,Object> getRaw(){return raw;}
    private String text(String key){Object value=raw.get(key);return value==null?null:String.valueOf(value);}
    public String getText(){return text("text");}
    public String getHtml(){return text("html");}
    public String getMarkdown(){return text("markdown");}
    public String getSvg(){return text("svg");}
    public String getPng(){return text("png");}
    public String getJpeg(){return text("jpeg");}
    public String getPdf(){return text("pdf");}
    public String getLatex(){return text("latex");}
    public Object getJson(){return raw.get("json");}
    public String getJavascript(){return text("javascript");}
    public Object getData(){return raw.get("data");}
    public Chart getChart(){return Chart.from(raw.get("chart"));}
    public List<String> formats(){return Collections.unmodifiableList(new ArrayList<>(raw.keySet()));}
}
