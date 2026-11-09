package ai.novita.sandbox.code_interpreter;

import java.util.*;

/** Typed chart output. Unknown fields and future chart types remain available in {@link #getRaw()}. */
public class Chart {
    public enum Type { LINE, SCATTER, BAR, PIE, BOX_AND_WHISKER, SUPERCHART, UNKNOWN }
    public enum Scale { LINEAR, DATETIME, CATEGORICAL, LOG, SYMLOG, LOGIT, FUNCTION, FUNCTIONLOG, ASINH, UNKNOWN }
    protected final Map<String,Object> raw;
    private final Type type;
    private Chart(Map<String,Object> raw,Type type){this.raw=immutableMap(raw);this.type=type;}
    public Type getType(){return type;}
    public String getTitle(){return string(raw,"title");}
    public Map<String,Object> getRaw(){return raw;}
    public List<?> getElements(){return list(raw,"elements");}

    static Chart from(Object value){
        if(!(value instanceof Map))return null;
        @SuppressWarnings("unchecked") Map<String,Object> data=(Map<String,Object>)value;
        String type=string(data,"type");
        if(type==null)return new Chart(data,Type.UNKNOWN);
        switch(type){
            case "line":return new LineChart(data);
            case "scatter":return new ScatterChart(data);
            case "bar":return new BarChart(data);
            case "pie":return new PieChart(data);
            case "box_and_whisker":return new BoxAndWhiskerChart(data);
            case "superchart":return new SuperChart(data);
            default:return new Chart(data,Type.UNKNOWN);
        }
    }
    public static class Chart2D extends Chart {
        private Chart2D(Map<String,Object> data,Type type){super(data,type);}
        public String getXLabel(){return string(raw,"x_label");}
        public String getYLabel(){return string(raw,"y_label");}
        public String getXUnit(){return string(raw,"x_unit");}
        public String getYUnit(){return string(raw,"y_unit");}
    }
    public static class PointChart extends Chart2D {
        private PointChart(Map<String,Object> data,Type type){super(data,type);}
        public List<?> getXTicks(){return list(raw,"x_ticks");}
        public List<?> getYTicks(){return list(raw,"y_ticks");}
        public List<String> getXTickLabels(){return strings(raw,"x_tick_labels");}
        public List<String> getYTickLabels(){return strings(raw,"y_tick_labels");}
        public Scale getXScale(){return scale(raw,"x_scale");}
        public Scale getYScale(){return scale(raw,"y_scale");}
        @Override public List<PointData> getElements(){return elements(raw,PointData::new);}
    }
    public static final class LineChart extends PointChart {private LineChart(Map<String,Object> data){super(data,Type.LINE);}}
    public static final class ScatterChart extends PointChart {private ScatterChart(Map<String,Object> data){super(data,Type.SCATTER);}}
    public static final class BarChart extends Chart2D {
        private BarChart(Map<String,Object> data){super(data,Type.BAR);}
        @Override public List<BarData> getElements(){return elements(raw,BarData::new);}
    }
    public static final class PieChart extends Chart {
        private PieChart(Map<String,Object> data){super(data,Type.PIE);}
        @Override public List<PieData> getElements(){return elements(raw,PieData::new);}
    }
    public static final class BoxAndWhiskerChart extends Chart2D {
        private BoxAndWhiskerChart(Map<String,Object> data){super(data,Type.BOX_AND_WHISKER);}
        @Override public List<BoxAndWhiskerData> getElements(){return elements(raw,BoxAndWhiskerData::new);}
    }
    public static final class SuperChart extends Chart {
        private SuperChart(Map<String,Object> data){super(data,Type.SUPERCHART);}
        public List<Chart> getCharts(){
            List<Chart> charts=new ArrayList<>();
            for(Object value:list(raw,raw.containsKey("data")?"data":"elements")){Chart chart=from(value);if(chart!=null)charts.add(chart);}
            return Collections.unmodifiableList(charts);
        }
        @Override public List<Chart> getElements(){return getCharts();}
    }
    public static class Element {
        protected final Map<String,Object> raw;
        private Element(Map<String,Object> value){raw=immutableMap(value);}
        public String getLabel(){return string(raw,"label");}
        public Map<String,Object> getRaw(){return raw;}
    }
    public static final class PointData extends Element {
        private PointData(Map<String,Object> value){super(value);}
        /** Coordinates are numbers or strings, as allowed by the chart protocol. */
        public List<List<?>> getPoints(){
            List<List<?>> points=new ArrayList<>();for(Object point:list(raw,"points"))if(point instanceof List)points.add((List<?>)point);
            return Collections.unmodifiableList(points);
        }
    }
    public static final class BarData extends Element {
        private BarData(Map<String,Object> value){super(value);}
        public String getValue(){return string(raw,"value");}
        public String getGroup(){return string(raw,"group");}
    }
    public static final class PieData extends Element {
        private PieData(Map<String,Object> value){super(value);}
        public Double getAngle(){return number(raw,"angle");}
        public Double getRadius(){return number(raw,"radius");}
    }
    public static final class BoxAndWhiskerData extends Element {
        private BoxAndWhiskerData(Map<String,Object> value){super(value);}
        public Double getMin(){return number(raw,"min");}
        public Double getFirstQuartile(){return number(raw,"first_quartile");}
        public Double getMedian(){return number(raw,"median");}
        public Double getThirdQuartile(){return number(raw,"third_quartile");}
        public Double getMax(){return number(raw,"max");}
        public List<Double> getOutliers(){List<Double> values=new ArrayList<>();for(Object value:list(raw,"outliers"))if(value instanceof Number)values.add(((Number)value).doubleValue());return Collections.unmodifiableList(values);}
    }
    private static String string(Map<String,Object> data,String key){Object value=data.get(key);return value==null?null:String.valueOf(value);}
    private static Double number(Map<String,Object> data,String key){Object value=data.get(key);return value instanceof Number?((Number)value).doubleValue():null;}
    private static List<?> list(Map<String,Object> data,String key){Object value=data.get(key);return value instanceof List?(List<?>)value:Collections.emptyList();}
    private static List<String> strings(Map<String,Object> data,String key){List<String> values=new ArrayList<>();for(Object value:list(data,key))values.add(String.valueOf(value));return Collections.unmodifiableList(values);}
    private static Scale scale(Map<String,Object> data,String key){try{return Scale.valueOf(string(data,key).toUpperCase(Locale.ROOT));}catch(IllegalArgumentException|NullPointerException e){return Scale.UNKNOWN;}}
    private static <T> List<T> elements(Map<String,Object> raw,java.util.function.Function<Map<String,Object>,T> factory){
        List<T> result=new ArrayList<>();for(Object value:list(raw,"elements"))if(value instanceof Map){@SuppressWarnings("unchecked") Map<String,Object> data=(Map<String,Object>)value;result.add(factory.apply(data));}
        return Collections.unmodifiableList(result);
    }
    static Map<String,Object> immutableMap(Map<String,Object> source){
        Map<String,Object> result=new LinkedHashMap<>();source.forEach((key,value)->result.put(key,immutable(value)));return Collections.unmodifiableMap(result);
    }
    private static Object immutable(Object value){
        if(value instanceof Map){@SuppressWarnings("unchecked") Map<String,Object> data=(Map<String,Object>)value;return immutableMap(data);}
        if(value instanceof List){List<Object> result=new ArrayList<>();for(Object item:(List<?>)value)result.add(immutable(item));return Collections.unmodifiableList(result);}
        return value;
    }
}
