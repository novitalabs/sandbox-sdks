package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ImageConfig contains the runtime defaults declared by an OCI image.
type ImageConfig struct {
	Env        map[string]string
	Workdir    string
	Entrypoint []string
	Cmd        []string
}

// ImageConfigResolver reads image defaults. It must honor ctx. Registry holds
// only credentials explicitly supplied for the image, never sandbox API keys.
type ImageConfigResolver func(ctx context.Context, image string, registry *RegistryConfig) (*ImageConfig, error)

type ImageConfigFailurePolicy string

const (
	ImageConfigWarn   ImageConfigFailurePolicy = "warn"
	ImageConfigRaise  ImageConfigFailurePolicy = "raise"
	ImageConfigIgnore ImageConfigFailurePolicy = "ignore"
)

// ImageConfigOptions applies only to registry reads, including token exchange.
// Transport can supply a private CA or proxy. The caller owns its lifecycle.
type ImageConfigOptions struct {
	Transport       http.RoundTripper
	OnConfigFailure ImageConfigFailurePolicy
}

// InheritImageConfig controls automatic restoration of image defaults at build
// time. It defaults to true for an image base. BuildOptions can override it.
func (b *TemplateBuilder) InheritImageConfig(enabled bool) *TemplateBuilder {
	b.spec["inheritImageConfig"] = enabled
	return b
}

var safeShellArgument = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func shellCommand(parts []string) string {
	quoted := make([]string, len(parts))
	for i, part := range parts {
		if safeShellArgument.MatchString(part) {
			quoted[i] = part
		} else {
			quoted[i] = shellQuote(part)
		}
	}
	return strings.Join(quoted, " ")
}
func inheritedEnvAllowed(name string) bool {
	switch name {
	case "HOSTNAME", "HOME", "PWD", "OLDPWD", "SHLVL", "_", "TERM":
		return false
	}
	return true
}

func prepareImageSpec(ctx context.Context, spec map[string]any, opts BuildOptions) (map[string]any, error) {
	return prepareImageSpecWithDiagnostic(ctx, spec, opts, nil)
}
func prepareImageSpecWithDiagnostic(ctx context.Context, spec map[string]any, opts BuildOptions, failure *string) (map[string]any, error) {
	if failure != nil {
		*failure = ""
	}
	configOptions := ImageConfigOptions{}
	if opts.ImageConfigOptions != nil {
		configOptions = *opts.ImageConfigOptions
	}
	policy := configOptions.OnConfigFailure
	if policy == "" {
		policy = ImageConfigWarn
	}
	if policy != ImageConfigWarn && policy != ImageConfigRaise && policy != ImageConfigIgnore {
		return nil, &InvalidArgumentError{Argument: "OnConfigFailure", Reason: "must be warn, raise or ignore"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := cloneTemplateSpec(spec)
	inherit := true
	if value, ok := out["inheritImageConfig"].(bool); ok {
		inherit = value
	}
	if opts.InheritImageConfig != nil {
		inherit = *opts.InheritImageConfig
	}
	delete(out, "inheritImageConfig")
	image, _ := out["image"].(string)
	if image == "" {
		image, _ = out["fromImage"].(string)
	}
	instructionKey := "instructions"
	if _, ok := out["steps"]; ok {
		instructionKey = "steps"
	}
	instructions, err := decodeInstructions(out[instructionKey])
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	literals := map[string]string{}
	groups := decodeEnvGroups(out["envDeclarations"])
	delete(out, "envDeclarations")
	if raw, ok := out["envVars"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &env); err != nil {
			return nil, err
		}
	}
	if len(groups) == 0 {
		for _, key := range envKeys(env) {
			groups = append(groups, envGroup{Values: map[string]string{key: env[key]}, Runtime: true})
		}
	}
	_, fromTemplate := out["templateID"]
	_, fromTemplateAlias := out["fromTemplate"]
	if image != "" && inherit && !fromTemplate && !fromTemplateAlias {
		resolver := opts.ImageConfigResolver
		if resolver == nil {
			resolver = func(ctx context.Context, image string, registry *RegistryConfig) (*ImageConfig, error) {
				return FetchImageConfigWithOptions(ctx, image, registry, configOptions)
			}
		}
		var registry *RegistryConfig
		if raw, ok := out["fromImageRegistry"]; ok {
			data, err := json.Marshal(raw)
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(data, &registry); err != nil {
				return nil, err
			}
		}
		config, readErr := resolver(ctx, image, registry)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if readErr != nil {
			if failure != nil {
				*failure = fmt.Sprintf("Could not read image configuration for %s: %v", image, readErr)
			}
			if policy == ImageConfigRaise {
				return nil, &ImageConfigError{Image: image, Reason: readErr.Error()}
			}
			if policy == ImageConfigWarn {
				message := fmt.Sprintf("Could not read image configuration for %s: %v. Building without inherited image defaults.", image, readErr)
				log.Print(message)
				if opts.OnLog != nil {
					opts.OnLog(BuildLog{Timestamp: time.Now(), Level: "warn", Message: message})
				}
			}
		} else if config != nil {
			merged := map[string]string{}
			for key, value := range config.Env {
				if inheritedEnvAllowed(key) {
					merged[key] = value
					literals[key] = value
				}
			}
			for key, value := range env {
				merged[key] = value
			}
			env = merged
			if config.Workdir != "" {
				instructions = append([]Instruction{{Type: "WORKDIR", Args: []string{config.Workdir}}}, instructions...)
			}
			if _, explicit := out["startCmd"]; !explicit {
				parts := append(append([]string{}, config.Entrypoint...), config.Cmd...)
				if len(parts) > 0 {
					out["startCmd"] = shellCommand(parts)
					if _, explicit := out["readyCmd"]; !explicit {
						out["readyCmd"] = "sleep 20"
					}
				}
			}
		}
	}
	if len(env) > 0 {
		out["envVars"] = env
	}
	force, _ := out["force"].(bool)
	instructions = addRuntimeEnv(instructions, literals, groups, force)
	out[instructionKey] = instructions
	return out, nil
}

type imageReference struct{ base, repository, reference string }

func parseImageReference(image string) (imageReference, error) {
	var result imageReference
	ref := strings.TrimSpace(image)
	if ref == "" {
		return result, fmt.Errorf("image reference cannot be empty")
	}
	scheme := "https"
	if strings.Contains(ref, "://") {
		parsed, err := url.Parse(ref)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
			return result, fmt.Errorf("invalid image reference")
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
			return result, fmt.Errorf("registry requires HTTPS (HTTP is supported for loopback fixtures)")
		}
		scheme = parsed.Scheme
		ref = parsed.Host + parsed.Path
	}
	host := "registry-1.docker.io"
	if slash := strings.Index(ref, "/"); slash >= 0 {
		first := ref[:slash]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			host = first
			ref = ref[slash+1:]
		}
	}
	switch host {
	case "docker.io", "index.docker.io":
		host = "registry-1.docker.io"
	}
	repository, reference := ref, "latest"
	if at := strings.Index(ref, "@"); at >= 0 {
		repository, reference = ref[:at], ref[at+1:]
		if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
			repository = repository[:colon]
		}
	} else if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		repository, reference = ref[:colon], ref[colon+1:]
	}
	if host == "registry-1.docker.io" && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	if repository == "" || reference == "" || strings.ContainsAny(repository+reference, " ?#\\") || strings.Contains(repository, "..") {
		return result, fmt.Errorf("invalid image reference")
	}
	result = imageReference{base: scheme + "://" + host, repository: repository, reference: reference}
	return result, nil
}
func loopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

const registryManifestAccept = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json"

type registryReader struct {
	client                    *http.Client
	reference                 imageReference
	username, password, token string
}
type registryHTTPError struct{ status int }

func (e *registryHTTPError) Error() string { return fmt.Sprintf("registry returned HTTP %d", e.status) }

// FetchImageConfig reads a Docker Registry V2 manifest and config blob for
// linux/amd64. Bare names use Docker Hub. Reads have a 15-second total timeout,
// retry transient HTTP failures, and support Basic or Bearer registry auth.
// Callers can supply a custom resolver for cloud-specific credential exchange.
func FetchImageConfig(ctx context.Context, image string, registry *RegistryConfig) (*ImageConfig, error) {
	return FetchImageConfigWithOptions(ctx, image, registry, ImageConfigOptions{})
}

func FetchImageConfigWithOptions(ctx context.Context, image string, registry *RegistryConfig, options ImageConfigOptions) (*ImageConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ref, err := parseImageReference(image)
	if err != nil {
		return nil, err
	}
	reader := registryReader{reference: ref, client: &http.Client{CheckRedirect: registryRedirect, Transport: options.Transport}}

	if registry != nil {
		reader.username, reader.password = registry.Username, registry.Password
		if registry.Type == "gcp" && registry.ServiceAccountJSON != "" {
			reader.username = "_json_key"
			reader.password = registry.ServiceAccountJSON
		}
		if reader.username == "" && registry.Type != "" && registry.Type != "registry" {
			return nil, fmt.Errorf("registry type %s needs an ImageConfigResolver for credential exchange", registry.Type)
		}
	}
	manifestURL := ref.base + "/v2/" + ref.repository + "/manifests/"
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	reference := ref.reference
	for depth := 0; depth < 4; depth++ {
		manifest.Config.Digest = ""
		manifest.Manifests = nil
		if err := reader.getJSON(ctx, manifestURL+url.PathEscape(reference), &manifest); err != nil {
			return nil, err
		}
		if len(manifest.Manifests) == 0 {
			break
		}
		reference = ""
		for _, entry := range manifest.Manifests {
			if entry.Platform.OS == "linux" && entry.Platform.Architecture == "amd64" {
				reference = entry.Digest
				break
			}
		}
		if reference == "" {
			return nil, fmt.Errorf("image has no linux/amd64 variant")
		}
	}
	if manifest.Config.Digest == "" {
		return nil, fmt.Errorf("image manifest contains no config blob")
	}
	var blob struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		Config       struct {
			Env        []string `json:"Env"`
			WorkingDir string   `json:"WorkingDir"`
			Entrypoint []string `json:"Entrypoint"`
			Cmd        []string `json:"Cmd"`
		} `json:"config"`
	}
	if err := reader.getJSON(ctx, ref.base+"/v2/"+ref.repository+"/blobs/"+url.PathEscape(manifest.Config.Digest), &blob); err != nil {
		return nil, err
	}
	if (blob.OS != "" && blob.OS != "linux") || (blob.Architecture != "" && blob.Architecture != "amd64") {
		return nil, fmt.Errorf("image config is not linux/amd64")
	}
	config := &ImageConfig{Env: map[string]string{}, Workdir: blob.Config.WorkingDir, Entrypoint: blob.Config.Entrypoint, Cmd: blob.Config.Cmd}
	for _, entry := range blob.Config.Env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && inheritedEnvAllowed(name) {
			config.Env[name] = value
		}
	}
	return config, nil
}
func (r *registryReader) getJSON(ctx context.Context, target string, out any) error {
	authenticated := false
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", registryManifestAccept)
		if r.token != "" {
			req.Header.Set("Authorization", "Bearer "+r.token)
		} else if r.username != "" {
			req.SetBasicAuth(r.username, r.password)
		}
		res, err := r.client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusUnauthorized && !authenticated {
			challenge := strings.Join(res.Header.Values("WWW-Authenticate"), ", ")
			res.Body.Close()
			if err := r.authenticate(ctx, challenge); err != nil {
				return err
			}
			authenticated = true
			attempt--
			continue
		}
		if res.StatusCode == http.StatusOK {
			err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
			res.Body.Close()
			return err
		}
		status, retryAfter := res.StatusCode, res.Header.Get("Retry-After")
		res.Body.Close()
		if (status != 429 && status != 500 && status != 502 && status != 503 && status != 504) || attempt == 2 {
			return &registryHTTPError{status}
		}
		delay := time.Duration(attempt+1) * 250 * time.Millisecond
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
			delay = time.Duration(seconds) * time.Second
			if delay > 3*time.Second {
				delay = 3 * time.Second
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("registry read failed")
}

func (r *registryReader) authenticate(ctx context.Context, challenge string) error {
	params := bearerChallenge(challenge)
	if params == nil {
		return &registryHTTPError{http.StatusUnauthorized}
	}
	endpoint, err := url.Parse(params["realm"])
	if err != nil || endpoint.Host == "" || endpoint.User != nil || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && loopbackHost(endpoint.Hostname()))) {
		return fmt.Errorf("invalid registry authentication endpoint")
	}
	query := endpoint.Query()
	if params["service"] != "" {
		query.Set("service", params["service"])
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + r.reference.repository + ":pull"
	}
	query.Set("scope", scope)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	registryURL, _ := url.Parse(r.reference.base)
	if r.username != "" && registryCredentialsAllowed(registryURL, endpoint) {
		req.SetBasicAuth(r.username, r.password)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &registryHTTPError{res.StatusCode}
	}
	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&token); err != nil {
		return err
	}
	r.token = token.Token
	if r.token == "" {
		r.token = token.AccessToken
	}
	if r.token == "" {
		return fmt.Errorf("registry authentication returned an empty token")
	}
	return nil
}

func sameRegistryOrigin(a, b *url.URL) bool {
	return a != nil && b != nil && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
func registryCredentialsAllowed(registry, realm *url.URL) bool {
	if sameRegistryOrigin(registry, realm) {
		return true
	}
	return registry != nil && realm != nil && registry.Scheme == "https" && registry.Host == "registry-1.docker.io" && realm.Scheme == "https" && realm.Host == "auth.docker.io"
}
func registryRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("too many registry redirects")
	}
	if len(via) == 0 {
		return nil
	}
	if !sameRegistryOrigin(req.URL, via[0].URL) {
		req.Header.Del("Authorization")
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("HTTPS registry redirect cannot downgrade transport")
	}
	if req.URL.Scheme != "https" && !(req.URL.Scheme == "http" && loopbackHost(req.URL.Hostname())) {
		return fmt.Errorf("insecure registry redirect")
	}
	return nil
}
