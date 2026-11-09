package core

import "strings"

// bearerChallenge splits challenges without treating quoted commas as separators.
func bearerChallenge(header string) map[string]string {
	parts := []string{}
	start := 0
	quoted, escaped := false, false
	for i, c := range header {
		if escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			parts = append(parts, header[start:i])
			start = i + 1
		}
	}
	parts = append(parts, header[start:])
	active := false
	var params map[string]string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// A scheme precedes its first parameter and is separated by whitespace.
		space := strings.IndexAny(part, " \t")
		eq := strings.IndexByte(part, '=')
		if space >= 0 && (eq < 0 || space < eq) {
			scheme := part[:space]
			rest := strings.TrimSpace(part[space:])
			// Whitespace immediately before '=' belongs to a parameter, not a scheme.
			if !strings.HasPrefix(rest, "=") {
				if active {
					return params
				}
				active = strings.EqualFold(scheme, "bearer")
				if active {
					params = map[string]string{}
				}
				part = rest
			}
		}
		if !active {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			var out strings.Builder
			escaped := false
			for _, c := range value[1:] {
				if escaped {
					out.WriteRune(c)
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == '"' {
					break
				} else {
					out.WriteRune(c)
				}
			}
			value = out.String()
		}
		params[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return params
}
