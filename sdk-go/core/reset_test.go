package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResetResponseTokenSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, body, want  string
		status            int
		success, hasToken bool
	}{
		{"rotated", `{"trafficAccessToken":"new"}`, "new", 200, true, true},
		{"cleared", `{"trafficAccessToken":""}`, "", 200, true, true},
		{"absent", `{}`, "old", 200, true, false},
		{"null", `{"trafficAccessToken":null}`, "old", 200, true, false},
		{"empty body", ``, "old", 204, true, false},
		{"non JSON", `ok`, "old", 200, true, false},
		{"wrong token type", `{"trafficAccessToken":123}`, "old", 200, true, false},
		{"conflict", `{"trafficAccessToken":"ignore"}`, "old", 409, false, false},
		{"not found", `{"message":"gone"}`, "old", 404, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := reportClient(true, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/sandboxes/demo-node/reset" {
					t.Errorf("path = %s", r.URL.Path)
				}
				return reportResponse(tc.status, tc.body), nil
			})
			s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", ClientID: "node", TrafficAccessToken: "old", EnvdAccessToken: "envd"}, &SandboxOpts{})
			if err != nil {
				t.Fatal(err)
			}
			success, err := s.Reset(context.Background(), ResetOptions{})
			if tc.status == 404 {
				var notFound *SandboxNotFoundError
				if !errors.As(err, &notFound) {
					t.Fatalf("error = %T %v", err, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if success != tc.success || s.TrafficAccessToken() != tc.want || s.AccessToken() != "envd" {
				t.Fatalf("success = %v, traffic = %q, envd = %q", success, s.TrafficAccessToken(), s.AccessToken())
			}
			if tc.status != 404 {
				result, err := (&SandboxNamespace{client: c}).ResetWithResult(context.Background(), "demo-node", ResetOptions{})
				if err != nil || result.Success != tc.success || (result.TrafficAccessToken != nil) != tc.hasToken {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
				if tc.hasToken && *result.TrafficAccessToken != tc.want {
					t.Errorf("returned token = %q", *result.TrafficAccessToken)
				}
				oldAPI, err := c.ResetSandbox(context.Background(), "demo-node", nil)
				if err != nil || oldAPI != tc.success {
					t.Fatalf("old API = %v %v", oldAPI, err)
				}
			}
		})
	}
}

func TestResetTrafficModeMatrixAndPayload(t *testing.T) {
	for _, initialPrivate := range []bool{false, true} {
		for _, public := range []bool{false, true} {
			c := reportClient(false, func(r *http.Request) (*http.Response, error) {
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"resume": false, "secure": false, "timeout": float64(90), "allowPublicTraffic": public}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("payload = %v, want %v", got, want)
				}
				return reportResponse(200, `{"trafficAccessToken":"rotated"}`), nil
			})
			initial := ""
			if initialPrivate {
				initial = "old"
			}
			s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", TrafficAccessToken: initial}, &SandboxOpts{})
			if err != nil {
				t.Fatal(err)
			}
			opts := ResetOptions{Resume: Bool(false), Secure: Bool(false), Timeout: 90 * time.Second, AllowPublicTraffic: Bool(public), Network: &SandboxNetworkConfig{AllowPublicTrafficOption: Bool(!public)}}
			ok, err := s.Reset(context.Background(), opts)
			if err != nil || !ok || s.TrafficAccessToken() != "rotated" {
				t.Fatalf("reset = %v %v, token = %q", ok, err, s.TrafficAccessToken())
			}
		}
	}
}

func TestResetNamespaceDoesNotMutateOldHandleOrInput(t *testing.T) {
	c := reportClient(false, func(r *http.Request) (*http.Response, error) {
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, map[string]any{"resume": true, "network": map[string]any{"allowPublicTraffic": false}}) {
			t.Errorf("payload = %v", got)
		}
		return reportResponse(200, `{"trafficAccessToken":"new"}`), nil
	})
	s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", TrafficAccessToken: "old"}, &SandboxOpts{})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"network": map[string]any{"allowPublicTraffic": false}}
	result, err := c.ResetSandboxWithResult(context.Background(), "demo", payload)
	if err != nil || !result.Success || result.TrafficAccessToken == nil || *result.TrafficAccessToken != "new" {
		t.Fatalf("result = %+v %v", result, err)
	}
	if _, mutated := payload["resume"]; mutated || s.TrafficAccessToken() != "old" {
		t.Fatalf("input/old handle mutated: %v, %q", payload, s.TrafficAccessToken())
	}
	ok, err := (&SandboxNamespace{client: c}).ResetWithOptions(context.Background(), "demo", ResetOptions{Network: &SandboxNetworkConfig{AllowPublicTrafficOption: Bool(false)}})
	if err != nil || !ok {
		t.Fatalf("old typed API = %v %v", ok, err)
	}
}

func TestResetTokenConcurrentReaders(t *testing.T) {
	c := reportClient(false, func(r *http.Request) (*http.Response, error) {
		return reportResponse(200, `{"trafficAccessToken":"new"}`), nil
	})
	s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", TrafficAccessToken: "old"}, &SandboxOpts{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.TrafficAccessToken()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		if _, err := s.Reset(context.Background(), ResetOptions{}); err != nil {
			t.Error(err)
		}
	}
	wg.Wait()
	if s.TrafficAccessToken() != "new" {
		t.Fatal("token was not updated")
	}
}

type delayedResetBody struct {
	io.ReadCloser
	release <-chan struct{}
}

func (b delayedResetBody) Read(p []byte) (int, error) {
	<-b.release
	return b.ReadCloser.Read(p)
}

func TestConcurrentResetsKeepLatestTrafficToken(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	c := reportClient(false, func(r *http.Request) (*http.Response, error) {
		n := calls.Add(1)
		response := reportResponse(200, fmt.Sprintf(`{"trafficAccessToken":"token-%d"}`, n))
		if n == 1 {
			response.Body = delayedResetBody{response.Body, releaseFirst}
			close(firstStarted)
		}
		return response, nil
	})
	s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", TrafficAccessToken: "initial"}, &SandboxOpts{})
	if err != nil {
		t.Fatal(err)
	}
	reset := func(done chan<- error) {
		ok, err := s.Reset(context.Background(), ResetOptions{})
		if err == nil && !ok {
			err = fmt.Errorf("reset did not succeed")
		}
		done <- err
	}
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go reset(firstDone)
	<-firstStarted
	go reset(secondDone)
	// Let an overlapping reset finish before releasing the old response.
	// A serialized reset waits until the first response has been consumed.
	select {
	case err := <-secondDone:
		secondDone <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	for _, done := range []<-chan error{firstDone, secondDone} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := s.TrafficAccessToken(); got != "token-2" {
		t.Fatalf("traffic token = %q, want latest server token token-2", got)
	}
}
