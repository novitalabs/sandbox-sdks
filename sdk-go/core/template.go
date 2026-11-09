package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type TemplateListOptions struct {
	TemplateType string
	TemplateID   string
	Public       *bool
	MinCPUCount  int
	MaxCPUCount  int
	MinMemoryMB  int
	MaxMemoryMB  int
	Page, Limit  int
}

func validateTemplateListOptions(o *TemplateListOptions) error {
	if o == nil {
		return nil
	}
	if o.TemplateType != "" && o.TemplateType != "template_build" && o.TemplateType != "snapshot_template" && o.TemplateType != "all" {
		return &InvalidArgumentError{Argument: "template_type", Reason: "must be template_build, snapshot_template, or all"}
	}
	if o.Page < 0 {
		return &InvalidArgumentError{Argument: "page", Reason: "must be >= 1"}
	}
	if o.Limit < 0 || o.Limit > 100 {
		return &InvalidArgumentError{Argument: "limit", Reason: "must be between 1 and 100"}
	}
	return nil
}
func (t *TemplateClient) requireModern(operation string) error {
	if t.client.connection.IsLegacyDomain {
		return &UnsupportedError{Operation: operation, Reason: "requires a modern domain"}
	}
	return nil
}

type TemplateListResult struct {
	Templates  []TemplateInfo `json:"templates"`
	Total      int            `json:"total,omitempty"`
	Page       int            `json:"page,omitempty"`
	Limit      int            `json:"limit,omitempty"`
	TotalPages int            `json:"totalPages,omitempty"`
}
type BuildOptions struct {
	Alias           string
	Tags            []string
	Timeout         time.Duration
	Spec            map[string]any
	PollInterval    time.Duration
	OnLog           func(BuildLog)
	ContextPath     string
	IgnorePatterns  []string
	ResolveSymlinks bool
	CPUCount        int
	MemoryMB        int
	SkipCache       bool
	// InheritImageConfig defaults to true. Set false for fully offline builds.
	InheritImageConfig *bool
	// ImageConfigResolver overrides registry access, for example for private registries.
	ImageConfigResolver ImageConfigResolver
	// ImageConfigOptions configures registry trust/transport and failure handling.
	ImageConfigOptions *ImageConfigOptions
}
type BuildLog struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type TemplateBuildStatus string

const (
	TemplateBuildWaiting  TemplateBuildStatus = "waiting"
	TemplateBuildBuilding TemplateBuildStatus = "building"
	TemplateBuildReady    TemplateBuildStatus = "ready"
	TemplateBuildError    TemplateBuildStatus = "error"
)

type TemplateBuilder struct {
	spec           map[string]any
	forceNextLayer bool
	err            error
}

type CopyOptions struct {
	ForceUpload     bool
	User            string
	Mode            os.FileMode
	ResolveSymlinks *bool
}

type CopyItem struct {
	Source      string
	Destination string
	Options     CopyOptions
}

type PackageInstallOptions struct {
	Global              bool
	Dev                 bool
	NoInstallRecommends bool
	FixMissing          bool
}

type TemplateGitCloneOptions struct {
	Path   string
	Branch string
	Depth  int
	User   string
}

type ReadyCommand string

type RegistryConfig struct {
	Type               string `json:"type"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	AWSAccessKeyID     string `json:"awsAccessKeyId,omitempty"`
	AWSSecretAccessKey string `json:"awsSecretAccessKey,omitempty"`
	AWSRegion          string `json:"awsRegion,omitempty"`
	ServiceAccountJSON string `json:"serviceAccountJson,omitempty"`
	TenancyOCID        string `json:"tenancyOcid,omitempty"`
	UserOCID           string `json:"userOcid,omitempty"`
	Fingerprint        string `json:"fingerprint,omitempty"`
	PrivateKey         string `json:"privateKey,omitempty"`
	Region             string `json:"region,omitempty"`
	AccessKeyID        string `json:"accessKeyId,omitempty"`
	SecretAccessKey    string `json:"secretAccessKey,omitempty"`
}
type Instruction struct {
	Type            string   `json:"type"`
	Args            []string `json:"args,omitempty"`
	Force           bool     `json:"force,omitempty"`
	ForceUpload     *bool    `json:"forceUpload,omitempty"`
	FilesHash       string   `json:"filesHash,omitempty"`
	ResolveSymlinks *bool    `json:"resolveSymlinks,omitempty"`
}

func NewTemplateBuilder() *TemplateBuilder { return &TemplateBuilder{spec: map[string]any{}} }
func (b *TemplateBuilder) instruction(typ string, args ...string) *TemplateBuilder {
	list, _ := b.spec["instructions"].([]Instruction)
	b.spec["instructions"] = append(list, Instruction{Type: strings.ToUpper(typ), Args: args, Force: b.forceNextLayer})
	return b
}
func (b *TemplateBuilder) FromImage(image string) *TemplateBuilder {
	if strings.TrimSpace(image) == "" {
		b.setError(&InvalidArgumentError{Argument: "image", Reason: "cannot be empty"})
		return b
	}
	if previous, ok := b.spec["image"].(string); ok && previous != image {
		// Credentials belong to the selected base, not to a later image/registry.
		delete(b.spec, "fromImageRegistry")
	}
	b.spec["image"] = image
	if b.forceNextLayer {
		b.spec["force"] = true
	}
	delete(b.spec, "templateID")
	return b
}
func (b *TemplateBuilder) FromDebianImage(v string) *TemplateBuilder {
	if v == "" {
		v = "stable"
	}
	return b.FromImage("debian:" + v)
}
func (b *TemplateBuilder) FromUbuntuImage(v string) *TemplateBuilder {
	if v == "" {
		v = "latest"
	}
	return b.FromImage("ubuntu:" + v)
}
func (b *TemplateBuilder) FromPythonImage(v string) *TemplateBuilder {
	if v == "" {
		v = "3"
	}
	return b.FromImage("python:" + v)
}
func (b *TemplateBuilder) FromNodeImage(v string) *TemplateBuilder {
	if v == "" {
		v = "lts"
	}
	return b.FromImage("node:" + v)
}
func (b *TemplateBuilder) FromBunImage(v string) *TemplateBuilder {
	if v == "" {
		v = "latest"
	}
	return b.FromImage("oven/bun:" + v)
}
func (b *TemplateBuilder) FromBaseImage() *TemplateBuilder {
	return b.FromImage("novita/base:latest")
}
func (b *TemplateBuilder) FromTemplate(id string) *TemplateBuilder {
	if strings.TrimSpace(id) == "" {
		b.setError(&InvalidArgumentError{Argument: "template", Reason: "cannot be empty"})
		return b
	}
	b.spec["templateID"] = id
	if b.forceNextLayer {
		b.spec["force"] = true
	}
	delete(b.spec, "image")
	delete(b.spec, "fromImageRegistry")
	return b
}

func (b *TemplateBuilder) FromAWSRegistry(image, accessKeyID, secretAccessKey, region string) *TemplateBuilder {
	b.FromImage(image)
	b.spec["fromImageRegistry"] = RegistryConfig{Type: "aws", AWSAccessKeyID: accessKeyID, AWSSecretAccessKey: secretAccessKey, AWSRegion: region}
	return b
}

func (b *TemplateBuilder) FromGCPRegistry(image, serviceAccountJSON string) *TemplateBuilder {
	b.FromImage(image)
	b.spec["fromImageRegistry"] = RegistryConfig{Type: "gcp", ServiceAccountJSON: serviceAccountJSON}
	return b
}

func (b *TemplateBuilder) FromOCIRegistry(image string, config RegistryConfig) *TemplateBuilder {
	b.FromImage(image)
	config.Type = "oci"
	b.spec["fromImageRegistry"] = config
	return b
}

func (b *TemplateBuilder) FromHuaweiRegistry(image, accessKeyID, secretAccessKey, region string) *TemplateBuilder {
	b.FromImage(image)
	b.spec["fromImageRegistry"] = RegistryConfig{Type: "huawei", AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey, Region: region}
	return b
}

func (b *TemplateBuilder) FromDockerfile(contentOrPath string) *TemplateBuilder {
	content := contentOrPath
	if !strings.ContainsAny(contentOrPath, "\r\n") {
		if raw, err := os.ReadFile(contentOrPath); err == nil {
			content = string(raw)
		}
	}
	fromCount := 0
	var entrypoint, defaultCommand *dockerfileStartup
	for _, line := range dockerfileLogicalLines(content) {
		sep := strings.IndexAny(line, " \t")
		if sep < 0 {
			continue
		}
		keyword, value := strings.ToUpper(line[:sep]), strings.TrimSpace(line[sep+1:])
		switch keyword {
		case "FROM":
			fromCount++
			if fromCount > 1 {
				b.setError(&InvalidArgumentError{Argument: "dockerfile", Reason: "multi-stage Dockerfiles are not supported"})
				return b
			}
			fields := strings.Fields(value)
			for len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
				fields = fields[1:]
			}
			if len(fields) > 0 {
				b.FromImage(fields[0])
			}
		case "RUN":
			command, err := dockerfileCommand(value)
			if err != nil {
				b.setError(err)
				return b
			}
			b.Run(command)
		case "CMD", "ENTRYPOINT":
			startup, err := parseDockerfileStartup(value)
			if err != nil {
				b.setError(err)
				return b
			}
			if keyword == "ENTRYPOINT" {
				entrypoint = startup
			} else {
				defaultCommand = startup
			}
		case "WORKDIR":
			b.Workdir(value)
		case "USER":
			b.User(value)
		case "ENV", "ARG":
			fields, err := dockerfileWords(value)
			if err != nil {
				b.setError(err)
				return b
			}
			values := map[string]string{}
			if len(fields) > 1 && !strings.Contains(fields[0], "=") {
				values[fields[0]] = strings.Join(fields[1:], " ")
			} else {
				for _, assignment := range fields {
					pair := strings.SplitN(assignment, "=", 2)
					if len(pair) == 2 {
						values[pair[0]] = pair[1]
					} else if keyword == "ARG" {
						values[pair[0]] = ""
					}
				}
			}
			b.SetEnvs(values, keyword == "ENV")
		case "COPY", "ADD":
			options := CopyOptions{}
			for strings.HasPrefix(value, "--") {
				flag, tail, ok := strings.Cut(value, " ")
				if !ok {
					b.setError(fmt.Errorf("invalid Dockerfile %s", keyword))
					return b
				}
				value = strings.TrimSpace(tail)
				name, arg, _ := strings.Cut(flag, "=")
				switch name {
				case "--chown":
					options.User = arg
				case "--chmod":
					mode, err := strconv.ParseUint(arg, 8, 32)
					if err != nil {
						b.setError(err)
						return b
					}
					options.Mode = os.FileMode(mode)
				default:
					b.setError(fmt.Errorf("unsupported Dockerfile copy flag %s", name))
					return b
				}
			}
			fields, err := dockerfileArguments(value)
			if err != nil || len(fields) < 2 {
				b.setError(fmt.Errorf("invalid Dockerfile %s arguments: %s", keyword, value))
				return b
			}
			for _, source := range fields[:len(fields)-1] {
				b.Copy(source, fields[len(fields)-1], options)
			}
		}
	}
	if fromCount == 0 {
		b.setError(&InvalidArgumentError{Argument: "dockerfile", Reason: "a FROM instruction is required"})
	}
	if entrypoint != nil || defaultCommand != nil {
		b.spec["startCmd"] = effectiveDockerfileStartup(entrypoint, defaultCommand)
		if _, explicit := b.spec["readyCmd"]; !explicit {
			b.ReadyCommand("sleep 20")
		}
	}
	return b
}

type dockerfileStartup struct {
	exec  bool
	args  []string
	shell string
}

func parseDockerfileStartup(value string) (*dockerfileStartup, error) {
	startup := &dockerfileStartup{exec: strings.HasPrefix(value, "["), shell: value}
	if startup.exec {
		args, err := dockerfileArguments(value)
		if err != nil {
			return nil, err
		}
		startup.args = args
	}
	return startup, nil
}

// Docker appends CMD argv to an exec-form ENTRYPOINT. A shell-form
// ENTRYPOINT runs on its own; shell-form CMD becomes /bin/sh -c when appended.
func effectiveDockerfileStartup(entrypoint, command *dockerfileStartup) string {
	if entrypoint != nil && !entrypoint.exec {
		return entrypoint.shell
	}
	var args []string
	if entrypoint != nil {
		args = append(args, entrypoint.args...)
	}
	if command != nil {
		if command.exec {
			args = append(args, command.args...)
		} else if len(args) > 0 {
			args = append(args, "/bin/sh", "-c", command.shell)
		} else {
			return command.shell
		}
	}
	return shellCommand(args)
}

func dockerfileCommand(value string) (string, error) {
	if !strings.HasPrefix(value, "[") {
		return value, nil
	}
	parts, err := dockerfileArguments(value)
	if err != nil {
		return "", err
	}
	return shellCommand(parts), nil
}
func dockerfileArguments(value string) ([]string, error) {
	if strings.HasPrefix(value, "[") {
		var parts []string
		if err := json.Unmarshal([]byte(value), &parts); err != nil {
			return nil, fmt.Errorf("invalid Dockerfile exec array: %w", err)
		}
		return parts, nil
	}
	return dockerfileWords(value)
}

// Split Dockerfile values while preserving spaces inside quoted arguments.
// This deliberately does not execute a shell or expand command substitutions.
func dockerfileWords(value string) ([]string, error) {
	var parts []string
	var word strings.Builder
	var quote rune
	escaped, active := false, false
	for _, r := range value {
		if escaped {
			if quote == '"' && r != '"' && r != '\\' {
				word.WriteRune('\\')
			}
			word.WriteRune(r)
			escaped = false
			active = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			active = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			active = true
			continue
		}
		if r == ' ' || r == '\t' {
			if active {
				parts = append(parts, word.String())
				word.Reset()
				active = false
			}
			continue
		}
		word.WriteRune(r)
		active = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated Dockerfile argument")
	}
	if active {
		parts = append(parts, word.String())
	}
	return parts, nil
}

func dockerfileLogicalLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var lines []string
	var current string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		continued := strings.HasSuffix(line, "\\")
		line = strings.TrimSpace(strings.TrimSuffix(line, "\\"))
		if current != "" {
			current += " "
		}
		current += line
		if !continued {
			lines = append(lines, current)
			current = ""
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// Env adds a build environment variable. Pass true to persist it at runtime.
func (b *TemplateBuilder) Env(key, value string, runtime ...bool) *TemplateBuilder {
	return b.SetEnvs(map[string]string{key: value}, runtime...)
}

// SetEnvs records one declaration group; expressions see the preceding environment.
// Runtime persistence is opt-in. Dockerfile ENV opts in, while ARG stays build-only.
func (b *TemplateBuilder) SetEnvs(values map[string]string, runtime ...bool) *TemplateBuilder {
	if len(values) == 0 {
		return b
	}
	persist := len(runtime) > 0 && runtime[0]
	group := envGroup{Values: map[string]string{}, Runtime: persist}
	keys := make([]string, 0, len(values))
	for k, v := range values {
		keys = append(keys, k)
		group.Values[k] = v
	}
	sort.Strings(keys)
	args := []string{}
	for _, key := range keys {
		args = append(args, key, values[key])
	}
	groups := decodeEnvGroups(b.spec["envDeclarations"])
	b.spec["envDeclarations"] = append(groups, group)
	if persist {
		env, _ := b.spec["envVars"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for k, v := range values {
			env[k] = v
		}
		b.spec["envVars"] = env
	}
	b.instruction("env", args...)
	return b
}
func (b *TemplateBuilder) Command(command string) *TemplateBuilder {
	b.spec["startCmd"] = command
	b.instruction("cmd", command)
	return b
}
func (b *TemplateBuilder) InstallPackages(packages ...string) *TemplateBuilder {
	b.spec["packages"] = append([]string(nil), packages...)
	for _, p := range packages {
		b.instruction("run", "apt-get install -y "+p)
	}
	return b
}
func (b *TemplateBuilder) ExposePorts(ports ...int) *TemplateBuilder {
	b.spec["ports"] = append([]int(nil), ports...)
	return b
}
func (b *TemplateBuilder) Run(command string) *TemplateBuilder { return b.instruction("run", command) }
func (b *TemplateBuilder) Workdir(path string) *TemplateBuilder {
	return b.instruction("workdir", path)
}
func (b *TemplateBuilder) User(user string) *TemplateBuilder { return b.instruction("user", user) }
func (b *TemplateBuilder) PatchCommand(command string) *TemplateBuilder {
	b.spec["preBootScript"] = command
	return b
}
func (b *TemplateBuilder) ReadyCommand(command string) *TemplateBuilder {
	b.spec["readyCmd"] = command
	return b
}
func (b *TemplateBuilder) RegistryCredentials(username, password string) *TemplateBuilder {
	b.spec["fromImageRegistry"] = RegistryConfig{Type: "registry", Username: username, Password: password}
	return b
}
func (b *TemplateBuilder) CopyFile(source, destination string) *TemplateBuilder {
	return b.Copy(source, destination, CopyOptions{})
}
func (b *TemplateBuilder) WriteFile(path, content string) *TemplateBuilder {
	files, _ := b.spec["files"].([]map[string]string)
	b.spec["files"] = append(files, map[string]string{"path": path, "content": content})
	b.instruction("write", path, content)
	return b
}
func (b *TemplateBuilder) With(values map[string]any) *TemplateBuilder {
	for k, v := range values {
		b.spec[k] = v
	}
	return b
}
func (b *TemplateBuilder) Spec() map[string]any {
	out := map[string]any{}
	for k, v := range b.spec {
		out[k] = v
	}
	return out
}

func (b *TemplateBuilder) buildSpec() map[string]any {
	out := b.Spec()
	instructions, _ := decodeInstructions(out["instructions"])
	out["instructions"] = addRuntimeEnv(instructions, nil, decodeEnvGroups(out["envDeclarations"]), b.forceNextLayer)
	delete(out, "envDeclarations")
	return out
}

func (b *TemplateBuilder) JSON() ([]byte, error) {
	if b.err != nil {
		return nil, b.err
	}
	return json.Marshal(serializeTemplateSpec(b.buildSpec()))
}

func (b *TemplateBuilder) Dockerfile() (string, error) {
	if b.err != nil {
		return "", b.err
	}
	if _, inherited := b.spec["templateID"]; inherited {
		return "", &InvalidArgumentError{Argument: "template", Reason: "inherited templates cannot be converted to Dockerfile"}
	}
	image, _ := b.spec["image"].(string)
	if image == "" {
		return "", &InvalidArgumentError{Argument: "image", Reason: "base image is required"}
	}
	var out strings.Builder
	out.WriteString("FROM " + image + "\n")
	instructions, err := decodeInstructions(b.spec["instructions"])
	if err != nil {
		return "", err
	}
	for _, instruction := range instructions {
		switch instruction.Type {
		case "ENV":
			var values []string
			for i := 0; i+1 < len(instruction.Args); i += 2 {
				values = append(values, instruction.Args[i]+"="+instruction.Args[i+1])
			}
			out.WriteString("ENV " + strings.Join(values, " ") + "\n")
		case "COPY":
			if len(instruction.Args) >= 2 {
				out.WriteString("COPY " + instruction.Args[0] + " " + instruction.Args[1] + "\n")
			}
		default:
			out.WriteString(instruction.Type + " " + strings.Join(instruction.Args, " ") + "\n")
		}
	}
	if start, _ := b.spec["startCmd"].(string); start != "" {
		out.WriteString("ENTRYPOINT " + start + "\n")
	}
	return out.String(), nil
}

func (b *TemplateBuilder) setError(err error) {
	if b.err == nil {
		b.err = err
	}
}

func (b *TemplateBuilder) Err() error { return b.err }

func (b *TemplateBuilder) Copy(source, destination string, opts CopyOptions) *TemplateBuilder {
	if err := validateTemplateSource(source); err != nil {
		b.setError(err)
		return b
	}
	if destination == "" {
		b.setError(&InvalidArgumentError{Argument: "copy destination", Reason: "cannot be empty"})
		return b
	}
	args := []string{source, destination, opts.User, ""}
	if opts.Mode != 0 {
		args[3] = fmt.Sprintf("%04o", opts.Mode.Perm())
	}
	var forceUpload *bool
	if opts.ForceUpload {
		force := true
		forceUpload = &force
	}
	list, _ := b.spec["instructions"].([]Instruction)
	b.spec["instructions"] = append(list, Instruction{
		Type:            "COPY",
		Args:            args,
		Force:           opts.ForceUpload || b.forceNextLayer,
		ForceUpload:     forceUpload,
		ResolveSymlinks: opts.ResolveSymlinks,
	})
	return b
}

func (b *TemplateBuilder) CopyItems(items ...CopyItem) *TemplateBuilder {
	for _, item := range items {
		b.Copy(item.Source, item.Destination, item.Options)
	}
	return b
}

func (b *TemplateBuilder) Remove(paths []string, force, recursive bool, user string) *TemplateBuilder {
	args := []string{"rm"}
	if recursive {
		args = append(args, "-r")
	}
	if force {
		args = append(args, "-f")
	}
	args = append(args, paths...)
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) Rename(source, destination string, force bool, user string) *TemplateBuilder {
	args := []string{"mv"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, source, destination)
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) MakeDir(paths []string, mode os.FileMode, user string) *TemplateBuilder {
	args := []string{"mkdir", "-p"}
	if mode != 0 {
		args = append(args, "-m", fmt.Sprintf("%04o", mode.Perm()))
	}
	args = append(args, paths...)
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) Symlink(source, destination, user string, force bool) *TemplateBuilder {
	args := []string{"ln", "-s"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, source, destination)
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) RunAs(command, user string) *TemplateBuilder {
	args := []string{command}
	if user != "" {
		args = append(args, user)
	}
	return b.instruction("RUN", args...)
}

func (b *TemplateBuilder) PipInstall(packages []string, global bool) *TemplateBuilder {
	args := []string{"pip", "install"}
	if !global {
		args = append(args, "--user")
	}
	if len(packages) == 0 {
		packages = []string{"."}
	}
	args = append(args, packages...)
	user := ""
	if global {
		user = "root"
	}
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) NPMInstall(packages []string, opts PackageInstallOptions) *TemplateBuilder {
	args := []string{"npm", "install"}
	if opts.Global {
		args = append(args, "-g")
	}
	if opts.Dev {
		args = append(args, "--save-dev")
	}
	args = append(args, packages...)
	user := ""
	if opts.Global {
		user = "root"
	}
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) BunInstall(packages []string, opts PackageInstallOptions) *TemplateBuilder {
	args := []string{"bun", "install"}
	if opts.Global {
		args = append(args, "-g")
	}
	if opts.Dev {
		args = append(args, "--dev")
	}
	args = append(args, packages...)
	user := ""
	if opts.Global {
		user = "root"
	}
	return b.RunAs(strings.Join(args, " "), user)
}

func (b *TemplateBuilder) AptInstall(packages []string, opts PackageInstallOptions) *TemplateBuilder {
	install := "DEBIAN_FRONTEND=noninteractive DEBCONF_NOWARNINGS=yes apt-get install -y"
	if opts.NoInstallRecommends {
		install += " --no-install-recommends"
	}
	if opts.FixMissing {
		install += " --fix-missing"
	}
	install += " " + strings.Join(packages, " ")
	return b.RunAs("apt-get update && "+install, "root")
}

func (b *TemplateBuilder) GitClone(repository string, opts TemplateGitCloneOptions) *TemplateBuilder {
	args := []string{"git", "clone", repository}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch, "--single-branch")
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	if opts.Path != "" {
		args = append(args, opts.Path)
	}
	return b.RunAs(strings.Join(args, " "), opts.User)
}

func (b *TemplateBuilder) AddMCPServer(servers ...string) *TemplateBuilder {
	if base, _ := b.spec["templateID"].(string); base != "mcp-gateway" {
		b.setError(&InvalidArgumentError{Argument: "servers", Reason: "MCP servers require the mcp-gateway base template"})
		return b
	}
	return b.RunAs("mcp-gateway pull "+strings.Join(servers, " "), "root")
}

func (b *TemplateBuilder) SkipCache() *TemplateBuilder {
	b.forceNextLayer = true
	return b
}

func (b *TemplateBuilder) StartCommand(command string, ready ReadyCommand) *TemplateBuilder {
	b.spec["startCmd"] = command
	b.spec["readyCmd"] = string(ready)
	return b
}

func WaitForPort(port int) ReadyCommand {
	return ReadyCommand(fmt.Sprintf("ss -tuln | grep :%d", port))
}

func WaitForURL(target string, statusCode int) ReadyCommand {
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return ReadyCommand(fmt.Sprintf("curl -s -o /dev/null -w \"%%{http_code}\" %s | grep -q \"%d\"", target, statusCode))
}

func WaitForProcess(name string) ReadyCommand {
	return ReadyCommand("pgrep " + name + " > /dev/null")
}

func WaitForFile(path string) ReadyCommand { return ReadyCommand("[ -f " + path + " ]") }

func WaitForTimeout(timeout time.Duration) ReadyCommand {
	seconds := int64(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return ReadyCommand(fmt.Sprintf("sleep %d", seconds))
}
func (t *TemplateClient) BuildTemplate(ctx context.Context, name string, b *TemplateBuilder, opts BuildOptions) (*BuildInfo, error) {
	if b == nil {
		return nil, &InvalidArgumentError{Argument: "builder", Reason: "template builder is required"}
	}
	if err := b.Err(); err != nil {
		return nil, err
	}
	opts.Spec = b.Spec()
	return t.build(ctx, name, opts, true)
}

// BuildTemplateInBackground uploads all local inputs and triggers the build,
// then returns without polling for completion.
func (t *TemplateClient) BuildTemplateInBackground(ctx context.Context, name string, b *TemplateBuilder, opts BuildOptions) (*BuildInfo, error) {
	if b == nil {
		return nil, &InvalidArgumentError{Argument: "builder", Reason: "template builder is required"}
	}
	if err := b.Err(); err != nil {
		return nil, err
	}
	opts.Spec = b.Spec()
	return t.build(ctx, name, opts, false)
}

type BuildInfo struct {
	// ImageConfigFailure records a nonfatal registry lookup failure for this
	// local build invocation, even when no OnLog callback was supplied.
	ImageConfigFailure string     `json:"-"`
	TemplateID         string     `json:"templateID"`
	BuildID            string     `json:"buildID"`
	Name               string     `json:"name,omitempty"`
	Status             string     `json:"status,omitempty"`
	Tags               []string   `json:"tags,omitempty"`
	Alias              string     `json:"alias,omitempty"`
	CreatedAt          *time.Time `json:"createdAt,omitempty"`
}
type BuildReason struct {
	Message    string     `json:"message"`
	Step       string     `json:"step,omitempty"`
	LogEntries []BuildLog `json:"logEntries,omitempty"`
}
type BuildStatus struct {
	TemplateID string              `json:"templateID"`
	BuildID    string              `json:"buildID"`
	Status     TemplateBuildStatus `json:"status"`
	Message    string              `json:"message,omitempty"`
	LogEntries []BuildLog          `json:"logEntries,omitempty"`
	Logs       []string            `json:"logs,omitempty"`
	Reason     *BuildReason        `json:"reason,omitempty"`
}

func (t *TemplateClient) requestBuild(ctx context.Context, name string, opts BuildOptions) (*BuildInfo, error) {
	cpu, mem := opts.CPUCount, opts.MemoryMB
	if cpu == 0 {
		cpu = 2
	}
	if mem == 0 {
		mem = 1024
	}
	body := map[string]any{"name": name, "cpuCount": cpu, "memoryMB": mem}
	if len(opts.Tags) > 0 {
		body["tags"] = opts.Tags
	}
	var out BuildInfo
	err := t.client.resource(ctx, http.MethodPost, "/v3/templates", body, &out)
	if err == nil {
		if out.Name == "" {
			out.Name = name
		}
		if out.Alias == "" {
			out.Alias = name
		}
	}
	return &out, err
}
func (t *TemplateClient) triggerBuild(ctx context.Context, info *BuildInfo, spec map[string]any) error {
	return t.client.resource(ctx, http.MethodPost, "/v2/templates/"+url.PathEscape(info.TemplateID)+"/builds/"+url.PathEscape(info.BuildID), spec, nil)
}
func tarFiles(paths []string, contextPath string, resolveSymlinks bool) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, path := range paths {
		archivePath := path
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if resolveSymlinks {
				path, err = filepath.EvalSymlinks(path)
				if err != nil {
					return nil, err
				}
				info, err = os.Stat(path)
				if err != nil {
					return nil, err
				}
			} else {
				link, err = os.Readlink(path)
				if err != nil {
					return nil, err
				}
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(contextPath, archivePath)
		if err != nil {
			return nil, err
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			_, copyErr := io.Copy(tw, file)
			closeErr := file.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fileHash(src, dest, contextPath string, ignorePatterns []string, resolveSymlinks bool) (string, error) {
	h := sha256.New()
	paths, err := collectTemplateFiles(src, contextPath, ignorePatterns)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no files found in %s", filepath.Join(contextPath, src))
	}
	io.WriteString(h, "COPY "+src+" "+dest)
	for _, p := range paths {
		rel, err := filepath.Rel(contextPath, p)
		if err != nil {
			return "", err
		}
		io.WriteString(h, filepath.ToSlash(rel))
		fi, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 && !resolveSymlinks {
			writePythonStat(h, fi)
			target, err := os.Readlink(p)
			if err != nil {
				return "", err
			}
			io.WriteString(h, target)
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		writePythonStat(h, st)
		if !st.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func writePythonStat(w io.Writer, info os.FileInfo) {
	mode := uint64(info.Mode())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		mode = uint64(stat.Mode)
	}
	io.WriteString(w, strconv.FormatUint(mode, 10))
	io.WriteString(w, strconv.FormatInt(info.Size(), 10))
}

func validateTemplateSource(src string) error {
	if src == "" {
		return &InvalidArgumentError{Argument: "copy source", Reason: "cannot be empty"}
	}
	if filepath.IsAbs(src) || filepath.VolumeName(src) != "" {
		return &InvalidArgumentError{Argument: "copy source", Reason: "absolute paths are not allowed"}
	}
	clean := filepath.Clean(src)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return &InvalidArgumentError{Argument: "copy source", Reason: "path escapes the context directory"}
	}
	return nil
}

func templateIgnorePatterns(contextPath string, explicit []string) ([]string, error) {
	patterns := append([]string(nil), explicit...)
	data, err := os.ReadFile(filepath.Join(contextPath, ".dockerignore"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, line)
			}
		}
	}
	return patterns, nil
}

func collectTemplateFiles(src, contextPath string, ignorePatterns []string) ([]string, error) {
	if err := validateTemplateSource(src); err != nil {
		return nil, err
	}
	contextPath, err := filepath.Abs(contextPath)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(contextPath, filepath.FromSlash(src)))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		if _, statErr := os.Lstat(filepath.Join(contextPath, filepath.FromSlash(src))); statErr == nil {
			matches = []string{filepath.Join(contextPath, filepath.FromSlash(src))}
		}
	}
	unique := map[string]struct{}{}
	for _, match := range matches {
		info, statErr := os.Lstat(match)
		if statErr != nil {
			return nil, statErr
		}
		if !info.IsDir() {
			if !templatePathIgnored(match, contextPath, ignorePatterns) {
				unique[match] = struct{}{}
			}
			continue
		}
		walkErr := filepath.Walk(match, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if templatePathIgnored(path, contextPath, ignorePatterns) {
				if info.IsDir() && path != match && !couldReinclude(path, contextPath, ignorePatterns) {
					return filepath.SkipDir
				}
				return nil
			}
			unique[path] = struct{}{}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, _ := filepath.Rel(contextPath, paths[i])
		b, _ := filepath.Rel(contextPath, paths[j])
		return filepath.ToSlash(a) < filepath.ToSlash(b)
	})
	return paths, nil
}

func couldReinclude(path, contextPath string, patterns []string) bool {
	rel, _ := filepath.Rel(contextPath, path)
	rel = filepath.ToSlash(rel) + "/"
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if strings.HasPrefix(pattern, "!") && strings.HasPrefix(strings.TrimPrefix(pattern, "!"), rel) {
			return true
		}
	}
	return false
}

func templatePathIgnored(path, contextPath string, patterns []string) bool {
	rel, err := filepath.Rel(contextPath, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	ignored := false
	for _, raw := range patterns {
		pattern := strings.TrimSpace(raw)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		if dockerPatternMatches(pattern, rel) {
			ignored = !negated
		}
	}
	return ignored
}

func dockerPatternMatches(pattern, rel string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "/")
	directoryOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return false
	}
	anchored := strings.Contains(pattern, "/")
	var expression strings.Builder
	expression.WriteString("^")
	if !anchored {
		expression.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				expression.WriteString(".*")
				i++
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	if directoryOnly {
		expression.WriteString("(?:/.*)?")
	} else {
		expression.WriteString("(?:/.*)?")
	}
	expression.WriteString("$")
	return regexp.MustCompile(expression.String()).MatchString(rel)
}

func cloneTemplateSpec(spec map[string]any) map[string]any {
	if spec == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return map[string]any{}
	}
	var clone map[string]any
	if json.Unmarshal(raw, &clone) != nil {
		return map[string]any{}
	}
	return clone
}

func decodeInstructions(value any) ([]Instruction, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var instructions []Instruction
	if err := json.Unmarshal(raw, &instructions); err != nil {
		return nil, &InvalidArgumentError{Argument: "instructions", Reason: err.Error()}
	}
	return instructions, nil
}

func (t *TemplateClient) prepareAndUploadInstructions(ctx context.Context, info *BuildInfo, spec map[string]any, opts BuildOptions) (map[string]any, error) {
	prepared := cloneTemplateSpec(spec)
	instructionValue := prepared["instructions"]
	if instructionValue == nil {
		instructionValue = prepared["steps"]
	}
	list, err := decodeInstructions(instructionValue)
	if err != nil {
		return nil, err
	}
	base := opts.ContextPath
	if base == "" {
		base = "."
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	patterns, err := templateIgnorePatterns(absBase, opts.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	for index := range list {
		in := &list[index]
		if !strings.EqualFold(in.Type, "copy") || len(in.Args) < 1 {
			continue
		}
		if len(in.Args) < 2 {
			return nil, &InvalidArgumentError{Argument: "copy destination", Reason: "cannot be empty"}
		}
		if err := validateTemplateSource(in.Args[0]); err != nil {
			return nil, err
		}
		resolve := opts.ResolveSymlinks
		if in.ResolveSymlinks != nil {
			resolve = *in.ResolveSymlinks
		}
		paths, err := collectTemplateFiles(in.Args[0], absBase, patterns)
		if err != nil {
			return nil, &FileUploadError{Path: in.Args[0], Reason: err.Error()}
		}
		digest, err := fileHash(in.Args[0], in.Args[1], absBase, patterns, resolve)
		if err != nil {
			return nil, &FileUploadError{Path: in.Args[0], Reason: err.Error()}
		}
		in.FilesHash = digest
		var link struct {
			URL     string `json:"url"`
			Present bool   `json:"present"`
		}
		err = t.client.resource(ctx, http.MethodGet, "/templates/"+url.PathEscape(info.TemplateID)+"/files/"+digest, nil, &link)
		if err != nil {
			return nil, err
		}
		forceUpload := in.ForceUpload != nil && *in.ForceUpload
		if link.Present && !forceUpload {
			continue
		}
		if link.URL == "" {
			return nil, &FileUploadError{Path: in.Args[0], Reason: "upload URL is empty"}
		}
		raw, err := tarFiles(paths, absBase, resolve)
		if err != nil {
			return nil, &FileUploadError{Path: in.Args[0], Reason: err.Error()}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, link.URL, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		res, err := t.client.http.Do(req)
		if err != nil {
			return nil, &FileUploadError{Path: in.Args[0], Reason: err.Error()}
		}
		_, _ = io.Copy(io.Discard, res.Body)
		closeErr := res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &FileUploadError{Path: in.Args[0], Reason: "upload failed: " + res.Status}
		}
		if closeErr != nil {
			return nil, &FileUploadError{Path: in.Args[0], Reason: closeErr.Error()}
		}
	}
	prepared["instructions"] = list
	if _, exists := prepared["steps"]; exists {
		prepared["steps"] = list
		delete(prepared, "instructions")
	}
	return prepared, nil
}

func (t *TemplateClient) waitBuild(ctx context.Context, info *BuildInfo, interval time.Duration, onLog func(BuildLog)) error {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	offset := 0
	for {
		var st BuildStatus
		path := "/templates/" + url.PathEscape(info.TemplateID) + "/builds/" + url.PathEscape(info.BuildID) + "/status?logsOffset=" + fmt.Sprint(offset)
		if err := t.client.resource(ctx, http.MethodGet, path, nil, &st); err != nil {
			return err
		}
		for _, entry := range st.LogEntries {
			if onLog != nil {
				onLog(entry)
			}
		}
		offset += len(st.LogEntries)
		switch st.Status {
		case TemplateBuildReady:
			info.Status = string(st.Status)
			return nil
		case TemplateBuildError:
			info.Status = string(st.Status)
			msg := "template build failed"
			if st.Reason != nil && st.Reason.Message != "" {
				msg = st.Reason.Message
			}
			return &BuildError{TemplateID: info.TemplateID, BuildID: info.BuildID, Reason: msg}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

type TemplateClient struct{ client *Client }

func (c *Client) Templates() *TemplateClient { return &TemplateClient{client: c} }
func (t *TemplateClient) List(ctx context.Context, opts *TemplateListOptions) (*TemplateListResult, error) {
	if err := validateTemplateListOptions(opts); err != nil {
		return nil, err
	}
	q := url.Values{}
	if opts != nil {
		if opts.TemplateType != "" {
			q.Set("template_type", opts.TemplateType)
		}
		if opts.Page > 0 {
			q.Set("page", fmt.Sprint(opts.Page))
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprint(opts.Limit))
		}
		if opts.TemplateID != "" {
			q.Set("template_id", opts.TemplateID)
		}
		if opts.Public != nil {
			q.Set("public", strconv.FormatBool(*opts.Public))
		}
		if opts.MinCPUCount > 0 {
			q.Set("min_cpu_count", strconv.Itoa(opts.MinCPUCount))
		}
		if opts.MaxCPUCount > 0 {
			q.Set("max_cpu_count", strconv.Itoa(opts.MaxCPUCount))
		}
		if opts.MinMemoryMB > 0 {
			q.Set("min_memory_mb", strconv.Itoa(opts.MinMemoryMB))
		}
		if opts.MaxMemoryMB > 0 {
			q.Set("max_memory_mb", strconv.Itoa(opts.MaxMemoryMB))
		}
	}
	var out TemplateListResult
	err := t.client.resource(ctx, http.MethodGet, "/v2/templates?"+q.Encode(), nil, &out)
	return &out, err
}
func (t *TemplateClient) Get(ctx context.Context, id string) (*TemplateInfo, error) {
	if err := t.requireModern("Template.Get"); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, &InvalidArgumentError{Argument: "templateID", Reason: "cannot be empty"}
	}
	var out TemplateInfo
	err := t.client.resource(ctx, http.MethodGet, "/templates/"+url.PathEscape(id), nil, &out)
	if err != nil {
		return nil, err
	}
	// JS/Python derive the effective build from builds[0], unconditionally
	// overriding any top-level buildID/buildStatus (template/index.ts:137-142):
	// no builds entry leaves both empty.
	if len(out.Builds) > 0 {
		out.BuildID = out.Builds[0].BuildID
		out.BuildStatus = out.Builds[0].Status
	} else {
		out.BuildID = ""
		out.BuildStatus = ""
	}
	return &out, nil
}
func (t *TemplateClient) Delete(ctx context.Context, id string) error {
	if id == "" {
		return &InvalidArgumentError{Argument: "templateID", Reason: "cannot be empty"}
	}
	return t.client.resource(ctx, http.MethodDelete, "/templates/"+url.PathEscape(id), nil, nil)
}
func (t *TemplateClient) Build(ctx context.Context, name string, opts BuildOptions) (*BuildInfo, error) {
	return t.build(ctx, name, opts, true)
}
func (t *TemplateClient) BuildInBackground(ctx context.Context, name string, opts BuildOptions) (*BuildInfo, error) {
	return t.build(ctx, name, opts, false)
}

func (t *TemplateClient) build(ctx context.Context, name string, opts BuildOptions, wait bool) (*BuildInfo, error) {
	if err := t.requireModern("Template.Build"); err != nil {
		return nil, err
	}
	if name == "" {
		name = opts.Alias
	}
	if name == "" {
		return nil, &InvalidArgumentError{Argument: "name", Reason: "cannot be empty"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	var imageConfigFailure string
	var err error
	opts.Spec, err = prepareImageSpecWithDiagnostic(ctx, opts.Spec, opts, &imageConfigFailure)
	if err != nil {
		return nil, err
	}
	info, err := t.requestBuild(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	info.ImageConfigFailure = imageConfigFailure
	prepared, err := t.prepareAndUploadInstructions(ctx, info, opts.Spec, opts)
	if err != nil {
		return nil, err
	}
	if opts.SkipCache {
		prepared["force"] = true
	}
	prepared = serializeTemplateSpec(prepared)
	if err := t.triggerBuild(ctx, info, prepared); err != nil {
		return nil, err
	}
	if wait {
		if err := t.waitBuild(ctx, info, opts.PollInterval, opts.OnLog); err != nil {
			return nil, err
		}
	}
	return info, nil
}

func serializeTemplateSpec(spec map[string]any) map[string]any {
	out := cloneTemplateSpec(spec)
	delete(out, "inheritImageConfig")
	delete(out, "envDeclarations")
	delete(out, "envVars")
	if value, ok := out["instructions"]; ok {
		out["steps"] = value
		delete(out, "instructions")
	}
	if value, ok := out["image"]; ok {
		out["fromImage"] = value
		delete(out, "image")
	}
	if value, ok := out["templateID"]; ok {
		out["fromTemplate"] = value
		delete(out, "templateID")
		// preBootScript only applies while provisioning a base image. The
		// control plane intentionally ignores it for inherited templates.
		delete(out, "preBootScript")
	}
	// JS/Python always emit steps (an array, possibly empty) and force (a
	// bool, possibly false) in the trigger body (template serialize()); a
	// caller-provided nil value is replaced like a missing key.
	if value, ok := out["steps"]; !ok || value == nil {
		out["steps"] = []any{}
	}
	if value, ok := out["force"]; !ok || value == nil {
		out["force"] = false
	}
	return out
}

type BuildStatusOptions struct{ LogsOffset int }

func (t *TemplateClient) BuildStatus(ctx context.Context, templateID, buildID string) (*BuildStatus, error) {
	return t.BuildStatusWithOptions(ctx, templateID, buildID, nil)
}
func (t *TemplateClient) BuildStatusWithOptions(ctx context.Context, templateID, buildID string, opts *BuildStatusOptions) (*BuildStatus, error) {
	if err := t.requireModern("Template.BuildStatus"); err != nil {
		return nil, err
	}
	var out BuildStatus
	path := "/templates/" + url.PathEscape(templateID) + "/builds/" + url.PathEscape(buildID) + "/status"
	if opts != nil {
		if opts.LogsOffset < 0 {
			return nil, &InvalidArgumentError{Argument: "logsOffset", Reason: "must be nonnegative"}
		}
		path += "?logsOffset=" + strconv.Itoa(opts.LogsOffset)
	}
	err := t.client.resource(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}
func (t *TemplateClient) Exists(ctx context.Context, alias string) (bool, error) {
	if err := t.requireModern("Template.Exists"); err != nil {
		return false, err
	}
	res, err := t.client.do(ctx, http.MethodGet, "/templates/aliases/"+url.PathEscape(alias), "", nil)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode == http.StatusForbidden {
		return true, nil
	}
	if err := t.client.checkResponse(res); err != nil {
		return false, err
	}
	return true, nil
}
func (t *TemplateClient) AssignTags(ctx context.Context, target string, tags []string) (*TemplateTagInfo, error) {
	if err := t.requireModern("Template.AssignTags"); err != nil {
		return nil, err
	}
	var out TemplateTagInfo
	err := t.client.resource(ctx, http.MethodPost, "/templates/tags", map[string]any{"target": target, "tags": tags}, &out)
	return &out, err
}
func (t *TemplateClient) RemoveTags(ctx context.Context, name string, tags []string) error {
	if err := t.requireModern("Template.RemoveTags"); err != nil {
		return err
	}
	return t.client.resource(ctx, http.MethodDelete, "/templates/tags", map[string]any{"name": name, "tags": tags}, nil)
}
func (t *TemplateClient) Tags(ctx context.Context, id string) ([]TemplateTag, error) {
	if err := t.requireModern("Template.Tags"); err != nil {
		return nil, err
	}
	var out []TemplateTag
	err := t.client.resource(ctx, http.MethodGet, "/templates/"+url.PathEscape(id)+"/tags", nil, &out)
	return out, err
}
