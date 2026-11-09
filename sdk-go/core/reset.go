package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ResetResult reports whether a reset succeeded and any updated traffic token.
type ResetResult struct {
	Success bool
	// Nil means the response did not supply a token. A non-nil empty string
	// explicitly clears a previously held token.
	TrafficAccessToken *string
}

// ResetSandboxWithResult is ResetSandbox with access to the response token.
// It does not update existing sandbox handles. Use Sandbox.Reset to update
// the handle used for subsequent requests.
func (c *Client) ResetSandboxWithResult(ctx context.Context, id string, payload map[string]interface{}) (*ResetResult, error) {
	body := make(map[string]interface{}, len(payload)+1)
	body["resume"] = true
	for key, value := range payload {
		body[key] = value
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(id)+"/reset", "", bytesReader(raw))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusConflict {
		return &ResetResult{}, nil
	}
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	raw, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	result := &ResetResult{Success: true}
	var response struct {
		TrafficAccessToken *string `json:"trafficAccessToken"`
	}
	// Match Python: legacy responses can be empty or contain no usable JSON
	// token. The HTTP success still stands; keep any previously held token.
	if json.Unmarshal(raw, &response) == nil {
		result.TrafficAccessToken = response.TrafficAccessToken
	}
	return result, nil
}

// ResetWithResult resets by ID and returns the token without modifying handles.
func (s *SandboxNamespace) ResetWithResult(ctx context.Context, id string, opts ResetOptions) (*ResetResult, error) {
	return s.client.ResetSandboxWithResult(ctx, id, resetPayload(opts))
}

// Reset resets this sandbox and updates its traffic access token when supplied.
// A conflicting reset returns false; errors and responses without a token leave
// the current token unchanged. Envd credentials are separate from traffic tokens.
func (s *Sandbox) Reset(ctx context.Context, opts ResetOptions) (bool, error) {
	// Keep request order and token updates together so a delayed response
	// cannot overwrite the token issued by a later reset on this handle.
	s.resetMu.Lock()
	defer s.resetMu.Unlock()

	result, err := s.client.ResetSandboxWithResult(ctx, s.sandboxID, resetPayload(opts))
	if err != nil {
		return false, err
	}
	if result.Success && result.TrafficAccessToken != nil {
		s.trafficTokenMu.Lock()
		s.trafficAccessToken = *result.TrafficAccessToken
		s.trafficTokenMu.Unlock()
	}
	return result.Success, nil
}

func resetPayload(opts ResetOptions) map[string]interface{} {
	body := map[string]interface{}{"resume": true}
	if opts.Resume != nil {
		body["resume"] = *opts.Resume
	}
	if opts.Timeout > 0 {
		body["timeout"] = int64(opts.Timeout / time.Second)
	}
	if opts.Secure != nil {
		body["secure"] = *opts.Secure
	}
	if opts.AllowPublicTraffic != nil {
		body["allowPublicTraffic"] = *opts.AllowPublicTraffic
	} else if opts.Network != nil {
		body["network"] = opts.Network
	}
	return body
}
