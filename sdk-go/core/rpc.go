package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// filesystemService is the Connect service name for envd's filesystem service.
const filesystemService = "filesystem.Filesystem"

// connectProtocolVersionHeader is required by Connect servers on unary calls.
const connectProtocolVersionHeader = "Connect-Protocol-Version"

// rpcClient speaks the Connect RPC protocol using JSON encoding.
//
// The JS and Python SDKs both configure their transports with JSON (rather
// than the binary protobuf wire format), so the Go SDK does the same and can
// therefore rely on encoding/json instead of generated protobuf code.
type rpcClient struct {
	baseURL string
	http    *http.Client
	headers map[string]string
}

// newRPCClient builds an rpcClient rooted at the envd URL of a sandbox.
func newRPCClient(baseURL string, headers map[string]string) *rpcClient {
	return &rpcClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{},
		headers: headers,
	}
}

// serviceCaller issues Connect calls against one envd service.
type serviceCaller struct {
	client  *rpcClient
	service string
}

// newServiceCaller builds a caller for a Connect service, such as
// "process.Process" or "filesystem.Filesystem".
func newServiceCaller(sandbox *Sandbox, service string, headers map[string]string) *serviceCaller {
	return &serviceCaller{
		client: &rpcClient{
			baseURL: sandbox.rpc.baseURL,
			http:    sandbox.rpc.http,
			headers: headers,
		},
		service: service,
	}
}

// call performs a unary RPC and decodes the response into resp.
func (c *serviceCaller) call(ctx context.Context, method string, req interface{}, resp interface{}) error {
	return c.client.callService(ctx, c.service, method, req, resp)
}

// stream performs a server-streaming RPC, invoking onMessage per message.
func (c *serviceCaller) stream(ctx context.Context, method string, req interface{}, onMessage func([]byte) error) error {
	return c.client.streamService(ctx, c.service, method, req, onMessage)
}

// msToDuration converts a millisecond count to a time.Duration.
func msToDuration(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// rpcError is a Connect error carrying the protocol-level code.
type rpcError struct {
	Code    string
	Message string
	status  int
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// connectErrorBody is the JSON shape Connect servers use for errors.
type connectErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Connect protocol error codes.
const (
	CodeCanceled           = "canceled"
	CodeUnknown            = "unknown"
	CodeInvalidArgument    = "invalid_argument"
	CodeDeadlineExceeded   = "deadline_exceeded"
	CodeNotFound           = "not_found"
	CodeAlreadyExists      = "already_exists"
	CodePermissionDenied   = "permission_denied"
	CodeResourceExhausted  = "resource_exhausted"
	CodeFailedPrecondition = "failed_precondition"
	CodeAborted            = "aborted"
	CodeOutOfRange         = "out_of_range"
	CodeUnimplemented      = "unimplemented"
	CodeInternal           = "internal"
	CodeUnavailable        = "unavailable"
	CodeDataLoss           = "data_loss"
	CodeUnauthenticated    = "unauthenticated"
)

// statusToCode maps an HTTP status onto a Connect error code, used when the
// server does not return a structured error body.
func statusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeInvalidArgument
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodePermissionDenied
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeAlreadyExists
	case http.StatusTooManyRequests:
		return CodeResourceExhausted
	case http.StatusGatewayTimeout, http.StatusServiceUnavailable:
		return CodeUnavailable
	default:
		return CodeUnknown
	}
}

// call performs a unary Connect RPC, encoding req as JSON.
func (c *rpcClient) call(ctx context.Context, method string, req interface{}, resp interface{}) error {
	return c.callService(ctx, filesystemService, method, req, resp)
}

// callService performs a unary Connect RPC against a named service.
func (c *rpcClient) callService(ctx context.Context, service, method string, req interface{}, resp interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/"+service+"/"+method,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}

	c.applyHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return decodeError(res)
	}

	if resp == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// applyHeaders attaches the common headers to an outgoing request.
func (c *rpcClient) applyHeaders(req *http.Request) {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set(connectProtocolVersionHeader, "1")
}

// decodeError converts a non-200 response into an *rpcError.
func decodeError(res *http.Response) error {
	bodyBytes, _ := io.ReadAll(res.Body)

	var body connectErrorBody
	if err := json.Unmarshal(bodyBytes, &body); err == nil && body.Code != "" {
		return &rpcError{Code: body.Code, Message: body.Message, status: res.StatusCode}
	}

	code := statusToCode(res.StatusCode)
	message := strings.TrimSpace(string(bodyBytes))
	if message == "" {
		message = res.Status
	}
	return &rpcError{Code: code, Message: message, status: res.StatusCode}
}

// envelopeFrame wraps a JSON message in the 5-byte Connect streaming envelope:
// one flag byte followed by a big-endian uint32 length.
func envelopeFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0 // no compression
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

// readEnvelope reads one message from a streaming response, returning io.EOF
// when the stream is exhausted. End-of-stream envelopes are signalled by a
// flag byte with the 0x02 bit set.
func readEnvelope(r *bufio.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(header[1:5])
	if header[0]&0x02 != 0 {
		// End-of-stream: the payload is a JSON error object, if anything.
		if length == 0 {
			return nil, io.EOF
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		var end struct {
			Error            *connectErrorBody `json:"error"`
			connectErrorBody                   // Accept older flat error fixtures as well.
		}
		if err := json.Unmarshal(payload, &end); err != nil {
			return nil, fmt.Errorf("invalid Connect trailer: %w", err)
		}
		body := end.Error
		if body == nil && end.Code != "" {
			body = &end.connectErrorBody
		}
		if body != nil {
			return nil, &rpcError{Code: body.Code, Message: body.Message}
		}
		return nil, io.EOF
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// streamCall opens a server-streaming Connect RPC and invokes onMessage for
// each decoded message. Returning an error from onMessage stops the stream.
func (c *rpcClient) streamCall(
	ctx context.Context,
	method string,
	req interface{},
	onMessage func(payload []byte) error,
) error {
	return c.streamService(ctx, filesystemService, method, req, onMessage)
}

// streamService opens a server-streaming Connect RPC against a named service.
func (c *rpcClient) streamService(
	ctx context.Context,
	service string,
	method string,
	req interface{},
	onMessage func(payload []byte) error,
) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/"+service+"/"+method,
		bytes.NewReader(envelopeFrame(body)),
	)
	if err != nil {
		return err
	}

	c.applyHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/connect+json")

	res, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return decodeError(res)
	}

	reader := bufio.NewReader(res.Body)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		payload, err := readEnvelope(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := onMessage(payload); err != nil {
			return err
		}
	}
}

// restClient performs JSON REST calls against envd (used for file upload and
// download, which envd exposes as plain HTTP endpoints).
type restClient struct {
	baseURL string
	http    *http.Client
	headers map[string]string
}

// newRESTClient builds a restClient rooted at the envd URL of a sandbox.
func newRESTClient(baseURL string, headers map[string]string) *restClient {
	return &restClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{},
		headers: headers,
	}
}

// do performs an HTTP request with the shared headers applied.
func (c *restClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.http.Do(req)
}

// timeoutClient returns the shared HTTP client with a per-call timeout.
func (c *restClient) withTimeout(d time.Duration) *http.Client {
	if d <= 0 {
		return c.http
	}
	return &http.Client{Timeout: d}
}
