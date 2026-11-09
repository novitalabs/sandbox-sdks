package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type envGroup struct {
	Values  map[string]string `json:"values"`
	Runtime bool              `json:"runtime"`
}

const envProfilePath = "/etc/profile.d/99-image-env.sh"
const envEnvironmentPath = "/etc/environment"
const envBasePath = "/etc/.novita-image-env-base.sh"
const envScriptPath = "/tmp/.novita-image-env.sh"

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func envKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if envNamePattern.MatchString(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
func decodeEnvGroups(raw any) []envGroup {
	if raw == nil {
		return nil
	}
	var groups []envGroup
	data, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(data, &groups)
	}
	return groups
}
func expandableEnvQuote(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\', '"', '`':
			out.WriteByte('\\')
			out.WriteByte(value[i])
		case '$':
			if reference := envReferencePattern.FindString(value[i:]); reference != "" {
				out.WriteString(reference)
				i += len(reference) - 1
			} else {
				out.WriteString(`\$`)
			}
		default:
			out.WriteByte(value[i])
		}
	}
	out.WriteByte('"')
	return out.String()
}

var envReferencePattern = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*|\{[A-Za-z_][A-Za-z0-9_]*\})`)

func envSnapshotCommand(groups []envGroup) string {
	names := map[string]string{}
	for _, g := range groups {
		for name := range g.Values {
			names[name] = ""
		}
	}
	keys := envKeys(names)
	if len(keys) == 0 {
		return ""
	}
	lines := []string{"set -e", "umask 077", ": > " + envBasePath, "command -p chmod 0600 " + envBasePath}
	for _, name := range keys {
		lines = append(lines, fmt.Sprintf(`if [ "${%s+x}" = x ]; then
  printf "export %s='" >> %s
  printf '%%s' "${%s}" | command -p sed "s/'/'\\\\''/g" >> %s
  printf "'\n" >> %s
else
  printf 'unset %s\n' >> %s
fi`, name, name, envBasePath, name, envBasePath, envBasePath, name, envBasePath))
	}
	return "command -p sh -c " + shellQuote(strings.Join(lines, "\n"))
}
func envFilesCommand(literals map[string]string, groups []envGroup) string {
	hasRuntime := len(envKeys(literals)) > 0
	for _, g := range groups {
		hasRuntime = hasRuntime || (g.Runtime && len(envKeys(g.Values)) > 0)
	}
	if !hasRuntime {
		return ""
	}
	lines := []string{"set -e"}
	if envSnapshotCommand(groups) != "" {
		lines = append(lines, ". "+shellQuote(envBasePath), "command -p rm -f "+shellQuote(envBasePath))
	}
	lines = append(lines, `if [ -f "`+envProfilePath+`" ]; then . "`+envProfilePath+`"; fi`, "command -p touch "+envProfilePath+" "+envEnvironmentPath, `__novita_build_env() {
  while [ "$#" -gt 0 ]; do export "$1=$2"; shift 2; done
}
__novita_runtime_env() {
  while [ "$#" -gt 0 ]; do
    export "$1=$2"
    printf "export %s='" "$1" >> /etc/profile.d/99-image-env.sh
    printf '%s' "$2" | command -p sed "s/'/'\\\\''/g" >> /etc/profile.d/99-image-env.sh
    printf "'\n" >> /etc/profile.d/99-image-env.sh
    command -p sed "/^$1=/d" /etc/environment > /etc/environment.tmp
    command -p cat /etc/environment.tmp > /etc/environment
    command -p rm -f /etc/environment.tmp
    case "$2" in
      *'"'*) ;;
      *) if [ "$(printf '%s' "$2" | command -p wc -l)" -eq 0 ]; then printf '%s=%s\n' "$1" "$2" >> /etc/environment; fi ;;
    esac
    shift 2
  done
}`)
	appendGroup := func(values map[string]string, runtime, literal bool) {
		args := []string{}
		for _, key := range envKeys(values) {
			value := expandableEnvQuote(values[key])
			if literal {
				value = shellQuote(values[key])
			}
			args = append(args, key, value)
		}
		if len(args) > 0 {
			function := "__novita_build_env"
			if runtime {
				function = "__novita_runtime_env"
			}
			lines = append(lines, function+" "+strings.Join(args, " "))
		}
	}
	appendGroup(literals, true, true)
	for _, group := range groups {
		appendGroup(group.Values, group.Runtime, false)
	}
	lines = append(lines, "command -p chmod 0644 "+envProfilePath+" "+envEnvironmentPath)
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
	return "echo " + encoded + " | command -p base64 -d > " + envScriptPath + " && command -p sh " + envScriptPath + " && command -p rm -f " + envScriptPath
}
func runtimeEnvCommand(envs map[string]string) string {
	groups := []envGroup{}
	for _, key := range envKeys(envs) {
		groups = append(groups, envGroup{Values: map[string]string{key: envs[key]}, Runtime: true})
	}
	return envFilesCommand(nil, groups)
}
func addRuntimeEnv(instructions []Instruction, literals map[string]string, groups []envGroup, force bool) []Instruction {
	command := envFilesCommand(literals, groups)
	if command == "" {
		return instructions
	}
	if snapshot := envSnapshotCommand(groups); snapshot != "" {
		instructions = append([]Instruction{{Type: "RUN", Args: []string{snapshot, "root"}}}, instructions...)
	}
	return append(instructions, Instruction{Type: "RUN", Args: []string{command, "root"}, Force: force})
}
