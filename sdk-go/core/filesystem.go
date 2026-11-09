package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// envd REST routes.
const envdFilesRoute = "/files"

// filesystemProtoType mirrors the protobuf FileType enum values.
type filesystemProtoType int

const (
	protoFileTypeUnspecified filesystemProtoType = 0
	protoFileTypeFile        filesystemProtoType = 1
	protoFileTypeDirectory   filesystemProtoType = 2
)

func (t *filesystemProtoType) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*t = filesystemProtoType(number)
		return nil
	}
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	switch strings.ToUpper(name) {
	case "1", "FILE", "FILE_TYPE_FILE":
		*t = protoFileTypeFile
	case "2", "DIRECTORY", "DIR", "FILE_TYPE_DIRECTORY":
		*t = protoFileTypeDirectory
	case "0", "UNSPECIFIED", "FILE_TYPE_UNSPECIFIED", "":
		*t = protoFileTypeUnspecified
	default:
		// Preserve forward compatibility with new protobuf enum names. Unknown
		// entry types are skipped by List, matching the existing numeric path.
		*t = protoFileTypeUnspecified
	}
	return nil
}

// mapFileType converts the protobuf enum into the SDK's FileType.
// Unspecified types have no mapping, matching the JS/Python behaviour.
func mapFileType(t filesystemProtoType) (FileType, bool) {
	switch t {
	case protoFileTypeFile:
		return FileTypeFile, true
	case protoFileTypeDirectory:
		return FileTypeDir, true
	default:
		return "", false
	}
}

// filesystemProtoEventType accepts both protobuf JSON enum names and numeric
// values because envd versions in the wild use both representations.
type filesystemProtoEventType int

func (t *filesystemProtoEventType) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*t = filesystemProtoEventType(number)
		return nil
	}

	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	switch strings.ToUpper(name) {
	case "1", "CREATE", "EVENT_TYPE_CREATE":
		*t = 1
	case "2", "WRITE", "EVENT_TYPE_WRITE":
		*t = 2
	case "3", "REMOVE", "EVENT_TYPE_REMOVE":
		*t = 3
	case "4", "RENAME", "EVENT_TYPE_RENAME":
		*t = 4
	case "5", "CHMOD", "EVENT_TYPE_CHMOD":
		*t = 5
	default:
		*t = 0
	}
	return nil
}

// protoTimestamp mirrors google.protobuf.Timestamp in JSON form.
type protoTimestamp struct {
	Seconds int64 `json:"seconds"`
	Nanos   int32 `json:"nanos"`
}

func (t *protoTimestamp) UnmarshalJSON(data []byte) error {
	value := strings.TrimSpace(string(data))
	if value == "" || value == "null" {
		return nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			t.Seconds, t.Nanos = parsed.Unix(), int32(parsed.Nanosecond())
			return nil
		}
		seconds, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid filesystem timestamp %q: %w", text, err)
		}
		if seconds > 1e11 {
			parsed := time.UnixMilli(seconds)
			t.Seconds, t.Nanos = parsed.Unix(), int32(parsed.Nanosecond())
		} else {
			t.Seconds = seconds
		}
		return nil
	}

	var raw struct {
		Seconds json.RawMessage `json:"seconds"`
		Nanos   int32           `json:"nanos"`
	}
	if err := json.Unmarshal(data, &raw); err == nil && len(raw.Seconds) > 0 {
		var seconds int64
		if err := json.Unmarshal(raw.Seconds, &seconds); err != nil {
			var text string
			if textErr := json.Unmarshal(raw.Seconds, &text); textErr != nil {
				return err
			}
			parsed, parseErr := strconv.ParseInt(text, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			seconds = parsed
		}
		t.Seconds, t.Nanos = seconds, raw.Nanos
		return nil
	}

	var numeric float64
	if err := json.Unmarshal(data, &numeric); err != nil {
		return err
	}
	if numeric > 1e11 {
		parsed := time.UnixMilli(int64(numeric))
		t.Seconds, t.Nanos = parsed.Unix(), int32(parsed.Nanosecond())
	} else {
		t.Seconds = int64(numeric)
		t.Nanos = int32((numeric - float64(t.Seconds)) * float64(time.Second))
	}
	return nil
}

// toTime converts a protobuf timestamp to a time.Time, or nil if absent.
func (t *protoTimestamp) toTime() *time.Time {
	if t == nil {
		return nil
	}
	res := time.Unix(t.Seconds, int64(t.Nanos)).UTC()
	return &res
}

// protoEntryInfo mirrors filesystem.EntryInfo.
type protoEntryInfo struct {
	Name          string              `json:"name"`
	Type          filesystemProtoType `json:"type"`
	Path          string              `json:"path"`
	Size          string              `json:"size"`
	Mode          uint32              `json:"mode"`
	Permissions   string              `json:"permissions"`
	Owner         string              `json:"owner"`
	Group         string              `json:"group"`
	ModifiedTime  *protoTimestamp     `json:"modifiedTime"`
	SymlinkTarget *string             `json:"symlinkTarget"`
}

// toEntryInfo maps a protobuf entry onto the SDK's EntryInfo.
func (e *protoEntryInfo) toEntryInfo() EntryInfo {
	fileType, _ := mapFileType(e.Type)

	size, _ := strconv.ParseInt(e.Size, 10, 64)

	return EntryInfo{
		Name:          e.Name,
		Type:          fileType,
		Path:          e.Path,
		Size:          size,
		Mode:          int32(e.Mode),
		Permissions:   e.Permissions,
		Owner:         e.Owner,
		Group:         e.Group,
		ModifiedTime:  e.ModifiedTime.toTime(),
		SymlinkTarget: e.SymlinkTarget,
	}
}

// FilesystemRequestOpts are the common options for filesystem operations.
type FilesystemRequestOpts struct {
	// User runs the operation as this user, affecting path resolution and ownership.
	User Username
	// RequestTimeoutMs overrides the default request timeout.
	RequestTimeoutMs *int
}

// FilesystemWriteOpts are options for writing files.
type FilesystemWriteOpts struct {
	FilesystemRequestOpts
	// Gzip compresses the upload.
	Gzip bool
	// UseOctetStream uploads with application/octet-stream instead of
	// multipart/form-data. Requires envd >= 0.5.7, otherwise it falls back.
	UseOctetStream bool
}

// FilesystemReadOpts are options for reading files.
type FilesystemReadOpts struct {
	FilesystemRequestOpts
	// Gzip requests a gzip-encoded response.
	Gzip bool
}

// FilesystemListOpts are options for listing a directory.
type FilesystemListOpts struct {
	FilesystemRequestOpts
	// Depth is how many directory levels to recurse into (minimum 1).
	Depth int
}
type FilesystemEventType string

const (
	FilesystemEventCreate FilesystemEventType = "create"
	FilesystemEventWrite  FilesystemEventType = "write"
	FilesystemEventRemove FilesystemEventType = "remove"
	FilesystemEventRename FilesystemEventType = "rename"
	FilesystemEventChmod  FilesystemEventType = "chmod"
)

type FilesystemEvent struct {
	Name string              `json:"name"`
	Type FilesystemEventType `json:"type"`
}
type WatchOptions struct {
	FilesystemRequestOpts
	Recursive bool
	OnEvent   func(FilesystemEvent)
	OnError   func(error)
}

type FilesystemWatcher struct {
	ID string
}
type WatchHandle struct {
	stop func()
	done chan struct{}
	err  error
}

// Stop cancels the watch. It is safe to call from an event callback.
func (h *WatchHandle) Stop() {
	if h != nil && h.stop != nil {
		h.stop()
	}
}

// Wait waits until the watch stops and releases its HTTP response.
func (h *WatchHandle) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.done:
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WriteEntry is a path/data pair for writing multiple files at once.
type WriteEntry struct {
	Path string
	Data []byte
}

// Filesystem provides access to the sandbox filesystem.
type Filesystem struct {
	sandbox *Sandbox
	rpc     *rpcClient
	rest    *restClient
}

// newFilesystem wires up the filesystem module for a sandbox.
func newFilesystem(sandbox *Sandbox) *Filesystem {
	return &Filesystem{
		sandbox: sandbox,
		rpc:     sandbox.rpc,
		rest:    sandbox.rest,
	}
}

func (f *Filesystem) WatchDir(ctx context.Context, path string, opts *WatchOptions) (*WatchHandle, error) {
	if opts == nil {
		opts = &WatchOptions{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Recursive && compareVersions(f.sandbox.envdVersion, EnvdVersionRecursiveWatch) < 0 {
		return nil, &UnsupportedError{Operation: "Filesystem.WatchDir recursive", Reason: "requires envd >= " + EnvdVersionRecursiveWatch}
	}
	streamCtx, cancel := context.WithCancel(ctx)
	h := &WatchHandle{done: make(chan struct{}), stop: cancel}
	started := make(chan struct{})
	var startOnce sync.Once
	headers := f.headersFor(opts.User)
	headers[KeepAlivePingHeader] = fmt.Sprint(KeepAlivePingIntervalSec)
	caller := newServiceCaller(f.sandbox, filesystemService, headers)
	go func() {
		defer close(h.done)
		defer cancel()
		err := caller.stream(streamCtx, "WatchDir", map[string]any{"path": path, "recursive": opts.Recursive}, func(data []byte) error {
			var msg struct {
				Start      *struct{} `json:"start"`
				Filesystem *struct {
					Name string                   `json:"name"`
					Type filesystemProtoEventType `json:"type"`
				} `json:"filesystem"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				return err
			}
			if msg.Start != nil {
				startOnce.Do(func() { close(started) })
			}
			if msg.Filesystem != nil && opts.OnEvent != nil {
				if typ := filesystemEventType(msg.Filesystem.Type); typ != "" {
					opts.OnEvent(FilesystemEvent{Name: msg.Filesystem.Name, Type: typ})
				}
			}
			return nil
		})
		if streamCtx.Err() != nil {
			err = streamCtx.Err()
		}
		h.err = handleFilesystemRPCError(err)
		if err != nil && streamCtx.Err() == nil && opts.OnError != nil {
			opts.OnError(h.err)
		}
	}()
	waitCtx, stopWait := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer stopWait()
	select {
	case <-started:
		return h, nil
	case <-h.done:
		select {
		case <-started:
			return h, nil
		default:
		}
		if h.err != nil {
			return nil, h.err
		}
		return nil, fmt.Errorf("watch stream ended before the start event")
	case <-waitCtx.Done():
		cancel()
		return nil, waitCtx.Err()
	}
}

// CreateWatcher creates a polling watcher and returns its server identifier.
func (f *Filesystem) CreateWatcher(ctx context.Context, path string, opts *WatchOptions) (*FilesystemWatcher, error) {
	if opts == nil {
		opts = &WatchOptions{}
	}
	if opts.Recursive && compareVersions(f.sandbox.envdVersion, EnvdVersionRecursiveWatch) < 0 {
		return nil, &UnsupportedError{Operation: "Filesystem.CreateWatcher recursive", Reason: "requires envd >= " + EnvdVersionRecursiveWatch}
	}
	user := f.resolveUser(f.sandbox.envdVersion, opts.User)
	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()
	caller := newServiceCaller(f.sandbox, filesystemService, f.headersFor(Username{Name: user}))
	var response struct {
		WatcherID string `json:"watcherId"`
	}
	if err := caller.call(ctx, "CreateWatcher", map[string]any{"path": path, "recursive": opts.Recursive}, &response); err != nil {
		return nil, handleFilesystemRPCError(err)
	}
	return &FilesystemWatcher{ID: response.WatcherID}, nil
}

// GetWatcherEvents drains events currently queued for a polling watcher.
func (f *Filesystem) GetWatcherEvents(ctx context.Context, watcher *FilesystemWatcher, opts *FilesystemRequestOpts) ([]FilesystemEvent, error) {
	if watcher == nil || watcher.ID == "" {
		return nil, &InvalidArgumentError{Argument: "watcher", Reason: "watcher ID is required"}
	}
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}
	user := f.resolveUser(f.sandbox.envdVersion, opts.User)
	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()
	caller := newServiceCaller(f.sandbox, filesystemService, f.headersFor(Username{Name: user}))
	var response struct {
		Events []struct {
			Name string                   `json:"name"`
			Type filesystemProtoEventType `json:"type"`
		} `json:"events"`
	}
	if err := caller.call(ctx, "GetWatcherEvents", map[string]any{"watcherId": watcher.ID}, &response); err != nil {
		return nil, handleFilesystemRPCError(err)
	}
	events := make([]FilesystemEvent, 0, len(response.Events))
	for _, event := range response.Events {
		if eventType := filesystemEventType(event.Type); eventType != "" {
			events = append(events, FilesystemEvent{Name: event.Name, Type: eventType})
		}
	}
	return events, nil
}

// RemoveWatcher removes a polling watcher.
func (f *Filesystem) RemoveWatcher(ctx context.Context, watcher *FilesystemWatcher, opts *FilesystemRequestOpts) error {
	if watcher == nil || watcher.ID == "" {
		return &InvalidArgumentError{Argument: "watcher", Reason: "watcher ID is required"}
	}
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}
	user := f.resolveUser(f.sandbox.envdVersion, opts.User)
	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()
	caller := newServiceCaller(f.sandbox, filesystemService, f.headersFor(Username{Name: user}))
	if err := caller.call(ctx, "RemoveWatcher", map[string]any{"watcherId": watcher.ID}, nil); err != nil {
		return handleFilesystemRPCError(err)
	}
	return nil
}

func filesystemEventType(value filesystemProtoEventType) FilesystemEventType {
	return map[filesystemProtoEventType]FilesystemEventType{1: FilesystemEventCreate, 2: FilesystemEventWrite, 3: FilesystemEventRemove, 4: FilesystemEventRename, 5: FilesystemEventChmod}[value]
}

// fsRequestOpts resolves the user for a request, defaulting it on envd versions
// that predate multi-user support.
func (f *Filesystem) resolveUser(envdVersion string, user Username) string {
	if user.Name == "" && compareVersions(envdVersion, EnvdDefaultUser) < 0 {
		return DefaultUsername
	}
	return user.Name
}

// headersFor merges the auth headers for a call with the sandbox base headers.
//
// Explicit users take precedence over the base headers so the Authorization
// header always reflects the requested user.
func (f *Filesystem) headersFor(user Username) map[string]string {
	headers := map[string]string{}
	for k, v := range f.sandbox.baseHeaders {
		headers[k] = v
	}
	for k, v := range authenticationHeader(f.sandbox.envdVersion, user.Name) {
		headers[k] = v
	}
	return headers
}

// Read reads a file and returns its raw bytes.
//
// Reading a directory or a missing file returns an error; a missing file maps
// to *FileNotFoundError.
func (f *Filesystem) Read(ctx context.Context, path string, opts *FilesystemReadOpts) ([]byte, error) {
	reader, err := f.ReadStream(ctx, path, opts)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// ReadStream opens a file without buffering it. The caller must close the
// returned reader. The request timeout applies until the reader is closed.
func (f *Filesystem) ReadStream(ctx context.Context, path string, opts *FilesystemReadOpts) (io.ReadCloser, error) {
	if opts == nil {
		opts = &FilesystemReadOpts{}
	}
	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	user := f.resolveUser(f.sandbox.envdVersion, opts.User)
	query := url.Values{"path": {path}}
	if user != "" {
		query.Set("username", user)
	}
	req, err := f.newRequest(ctx, http.MethodGet, envdFilesRoute, query, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	for k, v := range f.headersFor(opts.User) {
		req.Header.Set(k, v)
	}
	if opts.Gzip {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	res, err := f.rest.http.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		defer cancel()
		defer res.Body.Close()
		return nil, handleEnvdError(res)
	}
	body := res.Body
	if res.Header.Get("Content-Encoding") == "gzip" {
		reader, err := gzip.NewReader(body)
		if err != nil {
			body.Close()
			cancel()
			return nil, err
		}
		body = &gzipResponseBody{Reader: reader, body: body}
	}
	return &cancelOnCloseBody{ReadCloser: body, cancel: cancel}, nil
}

type gzipResponseBody struct {
	*gzip.Reader
	body io.Closer
}

func (b *gzipResponseBody) Close() error {
	err := b.Reader.Close()
	if closeErr := b.body.Close(); err == nil {
		err = closeErr
	}
	return err
}

// WriteStream writes one file without buffering the source. The caller retains
// ownership of data and must close it if needed. Gzip applies to octet uploads;
// older envd versions use multipart instead.
func (f *Filesystem) WriteStream(ctx context.Context, path string, data io.Reader, opts *FilesystemWriteOpts) (*WriteInfo, error) {
	if data == nil {
		return nil, &InvalidArgumentError{Argument: "data", Reason: "reader is required"}
	}
	if opts == nil {
		opts = &FilesystemWriteOpts{}
	}
	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()
	query := url.Values{}
	if user := f.resolveUser(f.sandbox.envdVersion, opts.User); user != "" {
		query.Set("username", user)
	}
	octet := opts.UseOctetStream && compareVersions(f.sandbox.envdVersion, EnvdOctetStreamUpload) >= 0
	var body io.Reader = data
	contentType := "application/octet-stream"
	encoding := ""
	if octet {
		query.Set("path", path)
	}
	if !octet || opts.Gzip {
		reader, writer := io.Pipe()
		defer reader.Close()
		body = reader
		if octet {
			encoding = "gzip"
			go func() {
				compressor := gzip.NewWriter(writer)
				_, err := io.Copy(compressor, data)
				if closeErr := compressor.Close(); err == nil {
					err = closeErr
				}
				writer.CloseWithError(err)
			}()
		} else {
			multipartWriter := multipart.NewWriter(writer)
			contentType = multipartWriter.FormDataContentType()
			go func() {
				part, err := multipartWriter.CreateFormFile("file", path)
				if err == nil {
					_, err = io.Copy(part, data)
				}
				if closeErr := multipartWriter.Close(); err == nil {
					err = closeErr
				}
				writer.CloseWithError(err)
			}()
		}
	}
	// NopCloser keeps Transport from closing a caller-owned reader.
	req, err := f.newRequest(ctx, http.MethodPost, envdFilesRoute, query, io.NopCloser(body))
	if err != nil {
		return nil, err
	}
	for k, v := range f.headersFor(opts.User) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	written, err := f.sendWrite(req)
	if err != nil {
		return nil, err
	}
	return &written[0], nil
}

// ReadText reads a file and returns its contents as a string.
func (f *Filesystem) ReadText(ctx context.Context, path string, opts *FilesystemReadOpts) (string, error) {
	data, err := f.Read(ctx, path, opts)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Write writes data to a file, creating parent directories as needed.
//
// Writing to an existing file overwrites it.
func (f *Filesystem) Write(ctx context.Context, path string, data []byte, opts *FilesystemWriteOpts) (*WriteInfo, error) {
	if opts == nil {
		opts = &FilesystemWriteOpts{}
	}

	results, err := f.WriteFiles(ctx, []WriteEntry{{Path: path, Data: data}}, opts)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("expected to receive information about written file")
	}
	return &results[0], nil
}

// WriteText is a convenience wrapper around Write for string content.
func (f *Filesystem) WriteText(ctx context.Context, path, content string, opts *FilesystemWriteOpts) (*WriteInfo, error) {
	return f.Write(ctx, path, []byte(content), opts)
}

// WriteFiles writes several files in one request when possible.
//
// A single octet-stream upload per file is used when supported; otherwise all
// files are sent as one multipart/form-data request.
func (f *Filesystem) WriteFiles(ctx context.Context, files []WriteEntry, opts *FilesystemWriteOpts) ([]WriteInfo, error) {
	if opts == nil {
		opts = &FilesystemWriteOpts{}
	}
	if len(files) == 0 {
		return []WriteInfo{}, nil
	}

	user := f.resolveUser(f.sandbox.envdVersion, opts.User)

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	supportsOctetStream := compareVersions(f.sandbox.envdVersion, EnvdOctetStreamUpload) >= 0
	if opts.UseOctetStream && supportsOctetStream {
		return f.writeOctetStream(ctx, files, user, opts)
	}
	return f.writeMultipart(ctx, files, user, opts)
}

// writeOctetStream uploads each file as its own octet-stream request.
func (f *Filesystem) writeOctetStream(ctx context.Context, files []WriteEntry, user string, opts *FilesystemWriteOpts) ([]WriteInfo, error) {
	results := make([]WriteInfo, 0, len(files))

	for _, file := range files {
		body := file.Data
		contentEncoding := ""

		if opts.Gzip {
			var buf bytes.Buffer
			writer := gzip.NewWriter(&buf)
			if _, err := writer.Write(file.Data); err != nil {
				return nil, err
			}
			if err := writer.Close(); err != nil {
				return nil, err
			}
			body = buf.Bytes()
			contentEncoding = "gzip"
		}

		query := url.Values{}
		query.Set("path", file.Path)
		if user != "" {
			query.Set("username", user)
		}

		req, err := f.newRequest(ctx, http.MethodPost, envdFilesRoute, query, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		if contentEncoding != "" {
			req.Header.Set("Content-Encoding", contentEncoding)
		}

		written, err := f.sendWrite(req)
		if err != nil {
			return nil, err
		}
		results = append(results, written...)
	}

	return results, nil
}

// writeMultipart uploads all files in a single multipart/form-data request.
func (f *Filesystem) writeMultipart(ctx context.Context, files []WriteEntry, user string, opts *FilesystemWriteOpts) ([]WriteInfo, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.Path)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	query := url.Values{}
	if user != "" {
		query.Set("username", user)
	}

	req, err := f.newRequest(ctx, http.MethodPost, envdFilesRoute, query, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return f.sendWrite(req)
}

// newRequest builds an envd request with the sandbox headers applied.
func (f *Filesystem) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	target := f.rest.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	for k, v := range f.headersFor(Username{}) {
		req.Header.Set(k, v)
	}
	return req, nil
}

// sendWrite executes a write request and decodes the WriteInfo list.
func (f *Filesystem) sendWrite(req *http.Request) ([]WriteInfo, error) {
	client := f.rest.http

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, handleEnvdError(res)
	}

	var written []WriteInfo
	if err := json.NewDecoder(res.Body).Decode(&written); err != nil {
		return nil, err
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("expected to receive information about written file")
	}

	return written, nil
}

// List lists the entries of a directory.
func (f *Filesystem) List(ctx context.Context, path string, opts *FilesystemListOpts) ([]EntryInfo, error) {
	if opts == nil {
		opts = &FilesystemListOpts{Depth: 1}
	}
	depth := opts.Depth
	if depth == 0 {
		depth = 1
	}
	if depth < 0 {
		return nil, &InvalidArgumentError{Argument: "depth", Reason: "depth should be at least one"}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	req := map[string]interface{}{
		"path":  path,
		"depth": depth,
	}

	var res struct {
		Entries []*protoEntryInfo `json:"entries"`
	}
	if err := client.call(ctx, "ListDir", req, &res); err != nil {
		return nil, handleFilesystemRPCError(err)
	}

	entries := make([]EntryInfo, 0, len(res.Entries))
	for _, e := range res.Entries {
		info := e.toEntryInfo()
		if info.Type == "" {
			continue
		}
		entries = append(entries, info)
	}
	return entries, nil
}

// MakeDir creates a directory and any missing parents.
//
// It returns true when the directory was created and false when it already existed.
func (f *Filesystem) MakeDir(ctx context.Context, path string, opts *FilesystemRequestOpts) (bool, error) {
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	var res struct {
		Entry *protoEntryInfo `json:"entry"`
	}
	err := client.call(ctx, "MakeDir", map[string]interface{}{"path": path}, &res)
	if err != nil {
		if rpc, ok := err.(*rpcError); ok && rpc.Code == CodeAlreadyExists {
			return false, nil
		}
		return false, handleFilesystemRPCError(err)
	}
	return true, nil
}

// Rename moves or renames a file or directory.
func (f *Filesystem) Rename(ctx context.Context, oldPath, newPath string, opts *FilesystemRequestOpts) (*EntryInfo, error) {
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	req := map[string]interface{}{
		"source":      oldPath,
		"destination": newPath,
	}

	var res struct {
		Entry *protoEntryInfo `json:"entry"`
	}
	if err := client.call(ctx, "Move", req, &res); err != nil {
		return nil, handleFilesystemRPCError(err)
	}
	if res.Entry == nil {
		return nil, fmt.Errorf("expected to receive information about moved object")
	}

	info := res.Entry.toEntryInfo()
	return &info, nil
}

// Remove removes a file or directory.
func (f *Filesystem) Remove(ctx context.Context, path string, opts *FilesystemRequestOpts) error {
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	if err := client.call(ctx, "Remove", map[string]interface{}{"path": path}, nil); err != nil {
		return handleFilesystemRPCError(err)
	}
	return nil
}

// Exists reports whether a file or directory exists.
func (f *Filesystem) Exists(ctx context.Context, path string, opts *FilesystemRequestOpts) (bool, error) {
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	var res struct {
		Entry *protoEntryInfo `json:"entry"`
	}
	err := client.call(ctx, "Stat", map[string]interface{}{"path": path}, &res)
	if err != nil {
		if rpc, ok := err.(*rpcError); ok && rpc.Code == CodeNotFound {
			return false, nil
		}
		return false, handleFilesystemRPCError(err)
	}
	return true, nil
}

// GetInfo returns metadata about a file or directory.
func (f *Filesystem) GetInfo(ctx context.Context, path string, opts *FilesystemRequestOpts) (*EntryInfo, error) {
	if opts == nil {
		opts = &FilesystemRequestOpts{}
	}

	ctx, cancel := f.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	headers := f.headersFor(opts.User)
	client := &rpcClient{baseURL: f.rpc.baseURL, http: f.rpc.http, headers: headers}

	var res struct {
		Entry *protoEntryInfo `json:"entry"`
	}
	if err := client.call(ctx, "Stat", map[string]interface{}{"path": path}, &res); err != nil {
		return nil, handleFilesystemRPCError(err)
	}
	if res.Entry == nil {
		return nil, fmt.Errorf("expected to receive information about the file or directory")
	}

	info := res.Entry.toEntryInfo()
	return &info, nil
}

// handleEnvdError maps envd REST errors onto SDK error types.
func handleEnvdError(res *http.Response) error {
	bodyBytes, _ := io.ReadAll(res.Body)
	message := errorMessageFromBody(bodyBytes)
	if message == "" {
		message = res.Status
	}

	switch res.StatusCode {
	case http.StatusNotFound:
		return &FileNotFoundError{Path: message}
	case http.StatusBadRequest:
		return &InvalidArgumentError{Argument: "", Reason: message}
	case http.StatusUnauthorized:
		return &AuthenticationError{Message: message}
	case http.StatusRequestEntityTooLarge, http.StatusInsufficientStorage:
		return &NotEnoughSpaceError{}
	case http.StatusBadGateway:
		return &TimeoutError{Operation: "request", Duration: "unknown"}
	default:
		return &SandboxError{Message: message, StatusCode: res.StatusCode}
	}
}

// handleFilesystemRPCError maps Connect RPC errors onto SDK error types.
func handleFilesystemRPCError(err error) error {
	rpc, ok := err.(*rpcError)
	if !ok {
		return err
	}

	switch rpc.Code {
	case CodeNotFound:
		return &FileNotFoundError{Path: rpc.Message}
	case CodeInvalidArgument:
		return &InvalidArgumentError{Argument: "", Reason: rpc.Message}
	case CodeUnauthenticated:
		return &AuthenticationError{Message: rpc.Message}
	case CodeUnavailable:
		return &SandboxNotFoundError{SandboxID: ""}
	case CodeCanceled:
		return &TimeoutError{Operation: "request", Duration: "unknown"}
	case CodeDeadlineExceeded:
		return &TimeoutError{Operation: "watch/command", Duration: "unknown"}
	default:
		return &SandboxError{Message: fmt.Sprintf("%s: %s", rpc.Code, rpc.Message)}
	}
}

// errorMessageFromBody extracts a human-readable message from an error payload.
func errorMessageFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var structured struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &structured); err == nil && structured.Message != "" {
		return structured.Message
	}

	var asString string
	if err := json.Unmarshal(body, &asString); err == nil {
		return asString
	}

	return string(body)
}
