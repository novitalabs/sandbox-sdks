package desktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/novitalabs/sandbox-sdks/sdk-go/core"
)

func TestLaunchRetainsPIDAndReleasesStream(t *testing.T) {
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Start") {
			t.Errorf("unexpected RPC %s", r.URL.Path)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var body struct {
			Process struct {
				Args []string `json:"args"`
			} `json:"process"`
		}
		if len(raw) < 5 {
			t.Error("missing request frame")
			return
		}
		if err = json.Unmarshal(raw[5:], &body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Process.Args) != 3 || body.Process.Args[2] != "gtk-launch 'firefox' 'https://example.test'" {
			t.Errorf("launch command=%v", body.Process.Args)
		}
		event := []byte(`{"event":{"start":{"pid":42}}}`)
		frame := make([]byte, 5+len(event))
		binary.BigEndian.PutUint32(frame[1:5], uint32(len(event)))
		copy(frame[5:], event)
		w.Write(frame)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(disconnected)
	}))
	defer server.Close()
	config, err := core.NewConnectionConfig(&core.SandboxOpts{SandboxURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := core.NewSandboxFromConfig(config, &core.SandboxInfo{SandboxID: "test", EnvdVersion: "0.5.7"})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Sandbox{Sandbox: inner}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := desktop.Launch(ctx, "firefox", "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if app.PID != 42 {
		t.Fatalf("PID=%d", app.PID)
	}
	select {
	case <-disconnected:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Launch discarded the handle without releasing its stream")
	}
}
