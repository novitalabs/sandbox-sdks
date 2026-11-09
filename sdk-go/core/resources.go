package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

var (
	secretNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	secretEnvPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	secretHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	// Transitional mappings retain compatibility with Python's IDNA codec,
	// including mapping sharp s to ss before encoding Unicode hostnames.
	secretHostIDNA = idna.New(idna.MapForLookup(), idna.Transitional(true), idna.BidiRule())
)

type VolumeInfo struct {
	VolumeID      string `json:"volumeID"`
	Name          string `json:"name"`
	Token         string `json:"token,omitempty"`
	QuotaSizeGiB  int64  `json:"quotaSizeGiB,omitempty"`
	UsedSizeBytes int64  `json:"usedSizeBytes,omitempty"`
}
type SecretBinding struct {
	Name        string   `json:"name"`
	Hosts       []string `json:"hosts"`
	Placeholder string   `json:"placeholder"`
	Description *string  `json:"description,omitempty"`
	HasSecret   bool     `json:"hasSecret"`
	Status      string   `json:"status"`
	UpdatedAt   *string  `json:"updatedAt,omitempty"`
}
type SecretWriteParams struct {
	Name        string   `json:"name"`
	Value       string   `json:"value"`
	Hosts       []string `json:"hosts"`
	Description *string  `json:"description,omitempty"`
}

func (c *Client) resource(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	var reader io.Reader
	if body != nil {
		reader = bytesReader(data)
	}
	res, err := c.do(ctx, method, path, "", reader)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return &NotFoundError{Resource: path}
	}
	if err := c.checkResponse(res); err != nil {
		return err
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}
func (c *Client) CreateVolume(ctx context.Context, name string, quota *int64) (*VolumeInfo, error) {
	// Volume operations are modern-only in JS/Python.
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Volume.Create", Reason: "requires a modern domain"}
	}
	if quota != nil && *quota < 0 {
		return nil, &InvalidArgumentError{Argument: "quotaGiB", Reason: "must be non-negative"}
	}
	body := map[string]interface{}{"name": name}
	if quota != nil {
		body["quotaSizeGiB"] = *quota
		body["quotaInodes"] = *quota * 1000
	}
	var out VolumeInfo
	err := c.resource(ctx, http.MethodPost, "/volumes", body, &out)
	return &out, err
}
func (c *Client) GetVolume(ctx context.Context, id string) (*VolumeInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Volume.Get", Reason: "requires a modern domain"}
	}
	var out VolumeInfo
	err := c.resource(ctx, http.MethodGet, "/volumes/"+url.PathEscape(id), nil, &out)
	return &out, err
}
func (c *Client) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Volume.List", Reason: "requires a modern domain"}
	}
	var out []VolumeInfo
	err := c.resource(ctx, http.MethodGet, "/volumes", nil, &out)
	return out, err
}

// DeleteVolume destroys a volume, matching JS/Python semantics: legacy
// domains are rejected, 404 reports false (already gone), any other error is
// returned, success reports true.
func (c *Client) DeleteVolume(ctx context.Context, id string) (bool, error) {
	if c.connection.IsLegacyDomain {
		return false, &UnsupportedError{Operation: "Volume.Delete", Reason: "requires a modern domain"}
	}
	err := c.resource(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(id), nil, nil)
	if err != nil {
		if _, ok := err.(*NotFoundError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UpdateVolumeQuota updates a volume quota. A nil quota sends an empty body
// like JS/Python (quotaSizeGiB undefined / None); a value sends
// {quotaSizeGiB, quotaInodes = size*1000}. Explicit negative sizes are
// rejected client-side like the Java SDK; zero is a valid explicit value.
func (c *Client) UpdateVolumeQuota(ctx context.Context, id string, quotaGiB *int64) (*VolumeInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Volume.UpdateQuota", Reason: "requires a modern domain"}
	}
	body := map[string]interface{}{}
	if quotaGiB != nil {
		if *quotaGiB < 0 {
			return nil, &InvalidArgumentError{Argument: "quotaGiB", Reason: "must be non-negative"}
		}
		body["quotaSizeGiB"] = *quotaGiB
		body["quotaInodes"] = *quotaGiB * 1000
	}
	var out VolumeInfo
	err := c.resource(ctx, http.MethodPatch, "/volumes/"+url.PathEscape(id), body, &out)
	return &out, err
}
func (c *Client) CreateSecret(ctx context.Context, p SecretWriteParams) (*SecretBinding, error) {
	if err := c.validateSecretOperation("Secret.Create"); err != nil {
		return nil, err
	}
	if err := validateSecretWriteParams(&p); err != nil {
		return nil, err
	}
	var out SecretBinding
	err := c.resource(ctx, http.MethodPost, "/secrets", p, &out)
	return &out, err
}
func (c *Client) ListSecrets(ctx context.Context) ([]SecretBinding, error) {
	if err := c.validateSecretOperation("Secret.List"); err != nil {
		return nil, err
	}
	var body struct {
		Secrets []SecretBinding `json:"secrets"`
	}
	err := c.resource(ctx, http.MethodGet, "/secrets", nil, &body)
	return body.Secrets, err
}
func (c *Client) GetSecret(ctx context.Context, name string) (*SecretBinding, error) {
	if err := c.validateSecretOperation("Secret.Get"); err != nil {
		return nil, err
	}
	if err := validateSecretName(name); err != nil {
		return nil, err
	}
	var out SecretBinding
	err := c.resource(ctx, http.MethodGet, "/secrets/"+url.PathEscape(name), nil, &out)
	return &out, err
}
func (c *Client) UpdateSecret(ctx context.Context, p SecretWriteParams) (*SecretBinding, error) {
	if err := c.validateSecretOperation("Secret.Update"); err != nil {
		return nil, err
	}
	if err := validateSecretWriteParams(&p); err != nil {
		return nil, err
	}
	var out SecretBinding
	err := c.resource(ctx, http.MethodPut, "/secrets/"+url.PathEscape(p.Name), map[string]interface{}{"value": p.Value, "hosts": p.Hosts, "description": p.Description}, &out)
	return &out, err
}
func (c *Client) DeleteSecret(ctx context.Context, name string) (string, error) {
	if err := c.validateSecretOperation("Secret.Delete"); err != nil {
		return "", err
	}
	if err := validateSecretName(name); err != nil {
		return "", err
	}
	var out struct {
		Deleted string `json:"deleted"`
	}
	err := c.resource(ctx, http.MethodDelete, "/secrets/"+url.PathEscape(name), nil, &out)
	return out.Deleted, err
}

func (c *Client) validateSecretOperation(operation string) error {
	if c.connection.IsLegacyDomain {
		return &UnsupportedError{Operation: operation, Reason: "requires a modern domain"}
	}
	return nil
}

func validateSecretWriteParams(p *SecretWriteParams) error {
	if err := validateSecretName(p.Name); err != nil {
		return err
	}
	if p.Value == "" {
		return &InvalidArgumentError{Argument: "value", Reason: "is required"}
	}
	hosts, err := normalizeSecretHosts(p.Hosts)
	if err != nil {
		return err
	}
	p.Hosts = hosts
	return nil
}

func validateSecretName(name string) error {
	if !secretNamePattern.MatchString(name) {
		return &InvalidArgumentError{Argument: "name", Reason: "must be 1-128 characters and contain only letters, numbers, dots, underscores, and hyphens"}
	}
	return nil
}

func normalizeSecretHosts(hosts []string) ([]string, error) {
	if len(hosts) == 0 {
		return nil, &InvalidArgumentError{Argument: "hosts", Reason: "is required"}
	}
	result := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		host := strings.TrimSpace(raw)
		if host == "" {
			return nil, &InvalidArgumentError{Argument: "host", Reason: "cannot be empty"}
		}
		if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#@:") {
			return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
		}
		wildcard := strings.HasPrefix(host, "*.")
		lookupHost := strings.TrimPrefix(host, "*.")
		if lookupHost == "" || strings.Contains(lookupHost, "*") {
			return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
		}
		if !utf8.ValidString(lookupHost) {
			return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
		}
		// Older Go toolchains can still accept ACE labels that decode to ASCII.
		// Check them before IDNA maps Unicode characters and loses the prefix.
		for _, label := range strings.FieldsFunc(norm.NFKC.String(lookupHost), func(r rune) bool {
			return r == '.' || r == '\u3002' || r == '\uff0e' || r == '\uff61'
		}) {
			if !strings.HasPrefix(strings.ToLower(label), "xn--") {
				continue
			}
			decoded, err := idna.Punycode.ToUnicode(strings.ToLower(label))
			if err != nil || strings.IndexFunc(decoded, func(r rune) bool { return r > unicode.MaxASCII }) < 0 {
				return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
			}
		}
		// Let IDNA perform Unicode case mapping; simple lowercasing can change
		// the target hostname. Keep the existing validation for ASCII names.
		if strings.IndexFunc(lookupHost, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
			asciiHost, err := secretHostIDNA.ToASCII(lookupHost)
			if err != nil {
				return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
			}
			host = asciiHost
			if wildcard {
				host = "*." + host
			}
		} else {
			host = strings.ToLower(host)
		}
		if !secretHostPattern.MatchString(host) {
			return nil, &InvalidArgumentError{Argument: "host", Reason: host + " must be a hostname or wildcard hostname"}
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result, nil
}

// ValidateSecretEnvs validates sandbox secret environment bindings and
// returns a defensive copy suitable for a create request.
func ValidateSecretEnvs(secrets, envs map[string]string) (map[string]string, error) {
	validated := make(map[string]string, len(secrets))
	for envName, secretName := range secrets {
		if !secretEnvPattern.MatchString(envName) {
			return nil, &InvalidArgumentError{Argument: "secrets", Reason: envName + " must be a valid environment variable name"}
		}
		if _, exists := envs[envName]; exists {
			return nil, &InvalidArgumentError{Argument: "secrets", Reason: "env vars and secrets both define " + envName}
		}
		if err := validateSecretName(secretName); err != nil {
			return nil, err
		}
		validated[envName] = secretName
	}
	return validated, nil
}
