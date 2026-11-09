package ai.novita.sandbox.core;

/**
 * Shell quoting helpers.
 *
 * <p>Single source of truth for turning arbitrary strings into POSIX shell
 * literals. Previously {@code Git} and {@code TemplateBuilder} each carried
 * their own copy and one of them was wrong, which allowed command injection.
 */
public final class Shell {
    private Shell() {}

    /**
     * Wraps {@code value} in single quotes, escaping embedded single quotes as
     * {@code '\''} (close quote, escaped quote, reopen quote).
     *
     * <p>The result is always a single shell word, so metacharacters such as
     * {@code $}, backticks, {@code ;}, {@code |} and newlines are inert.
     *
     * @param value the raw value; {@code null} becomes an empty literal
     * @return a safely quoted shell literal
     */
    public static String quote(String value) {
        if (value == null) return "''";
        return "'" + value.replace("'", "'\\''") + "'";
    }

    /**
     * Quotes each element and joins them with single spaces.
     *
     * @param values the raw values
     * @return a space-separated list of safely quoted shell literals
     */
    public static String quoteAll(String... values) {
        StringBuilder out = new StringBuilder();
        for (String value : values) {
            if (out.length() > 0) out.append(' ');
            out.append(quote(value));
        }
        return out.toString();
    }

    /**
     * Quotes each element of a list and joins them with single spaces.
     *
     * @param values the raw values
     * @return a space-separated list of safely quoted shell literals
     */
    public static String quoteAll(java.util.List<String> values) {
        if (values == null) return "";
        return quoteAll(values.toArray(new String[0]));
    }
}
