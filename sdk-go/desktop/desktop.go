package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
)

// Sandbox is a sandbox configured for desktop automation.
//
// It embeds the core sandbox, so lifecycle, filesystem, command, GUI
// automation, and VNC streaming APIs are available directly.
type Sandbox struct {
	*core.Sandbox
	Display string
	VNC     *VNCServer
	xfcePID int
}
type ScreenSize struct{ Width, Height int }
type CursorPosition struct{ X, Y int }
type MouseButton int

const (
	MouseLeft   MouseButton = 1
	MouseMiddle MouseButton = 2
	MouseRight  MouseButton = 3
)

type AppInfo struct {
	PID     int
	Command string
}

type CreateOptions struct {
	Sandbox    *core.SandboxOpts
	Resolution ScreenSize
	DPI        int
	Display    string
}

type WriteOptions struct {
	ChunkSize int
	Delay     time.Duration
}

type VNCStartOptions struct {
	VNCPort     int
	NoVNCPort   int
	RequireAuth bool
	WindowID    string
}

type VNCURLOptions struct {
	AutoConnect bool
	ViewOnly    bool
	Resize      string
	AuthKey     string
}

type WindowInfo struct {
	ID    string
	Title string
	PID   int
}

type App struct {
	PID     int
	Command string
	desktop *Sandbox
}

type VNCServer struct {
	desktop   *Sandbox
	vncPort   int
	noVNCPort int
	password  string
	handle    *core.CommandHandle
	running   bool
}

func (s *Sandbox) ScreenSize(ctx context.Context) (ScreenSize, error) {
	r, e := s.Commands.Run(ctx, "xdpyinfo | awk '/dimensions:/{print $2}'", nil)
	if e != nil {
		return ScreenSize{}, e
	}
	var w, h int
	if _, e = fmt.Sscanf(r.Stdout, "%dx%d", &w, &h); e != nil {
		return ScreenSize{}, e
	}
	return ScreenSize{w, h}, nil
}
func (s *Sandbox) Screenshot(ctx context.Context, path string) error {
	// Desktop images provision scrot, matching the JS/Python implementations;
	// retain gnome-screenshot as a fallback for older images.
	if _, err := s.Commands.Run(ctx, "command -v scrot >/dev/null 2>&1 && scrot --pointer "+shellQuote(path), nil); err == nil {
		return nil
	}
	_, err := s.Commands.Run(ctx, "gnome-screenshot -f "+shellQuote(path), nil)
	return err
}
func (s *Sandbox) MouseMove(ctx context.Context, x, y int) error {
	_, e := s.Commands.Run(ctx, fmt.Sprintf("xdotool mousemove --sync %d %d", x, y), nil)
	return e
}
func (s *Sandbox) Click(ctx context.Context, button MouseButton) error {
	_, e := s.Commands.Run(ctx, fmt.Sprintf("xdotool click %d", button), nil)
	return e
}
func (s *Sandbox) DoubleClick(ctx context.Context, button MouseButton) error {
	_, e := s.Commands.Run(ctx, fmt.Sprintf("xdotool click --repeat 2 %d", button), nil)
	return e
}
func (s *Sandbox) Scroll(ctx context.Context, clicks int) error {
	button := 5
	if clicks < 0 {
		button = 4
	}
	_, e := s.Commands.Run(ctx, fmt.Sprintf("xdotool click --repeat %d %d", abs(clicks), button), nil)
	return e
}
func (s *Sandbox) MouseDown(ctx context.Context, button MouseButton) error {
	_, err := s.Commands.Run(ctx, fmt.Sprintf("xdotool mousedown %d", button), nil)
	return err
}
func (s *Sandbox) MouseUp(ctx context.Context, button MouseButton) error {
	_, err := s.Commands.Run(ctx, fmt.Sprintf("xdotool mouseup %d", button), nil)
	return err
}
func (s *Sandbox) CursorPosition(ctx context.Context) (CursorPosition, error) {
	result, err := s.Commands.Run(ctx, "xdotool getmouselocation", nil)
	if err != nil {
		return CursorPosition{}, err
	}
	var x, y int
	if _, err := fmt.Sscanf(result.Stdout, "x:%d y:%d", &x, &y); err != nil {
		return CursorPosition{}, fmt.Errorf("parse cursor position %q: %w", strings.TrimSpace(result.Stdout), err)
	}
	return CursorPosition{X: x, Y: y}, nil
}
func (s *Sandbox) Drag(ctx context.Context, from, to CursorPosition, button MouseButton) error {
	if err := s.MouseMove(ctx, from.X, from.Y); err != nil {
		return err
	}
	if err := s.MouseDown(ctx, button); err != nil {
		return err
	}
	if err := s.MouseMove(ctx, to.X, to.Y); err != nil {
		return err
	}
	return s.MouseUp(ctx, button)
}
func (s *Sandbox) KeyDown(ctx context.Context, key string) error {
	_, e := s.Commands.Run(ctx, "xdotool keydown "+shellQuote(mapKey(key)), nil)
	return e
}
func (s *Sandbox) KeyUp(ctx context.Context, key string) error {
	_, e := s.Commands.Run(ctx, "xdotool keyup "+shellQuote(mapKey(key)), nil)
	return e
}
func (s *Sandbox) LaunchApp(ctx context.Context, command string) error {
	_, e := s.Commands.Run(ctx, command, nil)
	return e
}
func (s *Sandbox) TerminateApp(ctx context.Context, pid int) error {
	_, e := s.Commands.Run(ctx, fmt.Sprintf("kill %d", pid), nil)
	return e
}
func (s *Sandbox) FocusApp(ctx context.Context, pid int) error {
	_, e := s.Commands.Run(ctx, fmt.Sprintf("xdotool windowactivate $(xdotool search --pid %d | head -1)", pid), nil)
	return e
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func (s *Sandbox) KeyPress(ctx context.Context, key string) error {
	parts := strings.Split(key, "+")
	for index := range parts {
		parts[index] = mapKey(parts[index])
	}
	_, e := s.Commands.Run(ctx, "xdotool key "+shellQuote(strings.Join(parts, "+")), nil)
	return e
}
func (s *Sandbox) Type(ctx context.Context, text string) error {
	_, e := s.Commands.Run(ctx, "xdotool type -- "+shellQuote(text), nil)
	return e
}
func (s *Sandbox) Write(ctx context.Context, text string, opts WriteOptions) error {
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 25
	}
	if opts.Delay <= 0 {
		opts.Delay = 75 * time.Millisecond
	}
	runes := []rune(text)
	for start := 0; start < len(runes); start += opts.ChunkSize {
		end := start + opts.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		command := fmt.Sprintf("xdotool type --delay %d -- %s", opts.Delay.Milliseconds(), shellQuote(string(runes[start:end])))
		if _, err := s.Commands.Run(ctx, command, nil); err != nil {
			return err
		}
	}
	return nil
}
func (s *Sandbox) ListApps(ctx context.Context) ([]AppInfo, error) {
	r, e := s.Commands.Run(ctx, "ps -eo pid=,args=", nil)
	if e != nil {
		return nil, e
	}
	var out []AppInfo
	for _, line := range strings.Split(r.Stdout, "\n") {
		var p int
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 {
			p, e = strconv.Atoi(fields[0])
			if e != nil {
				continue
			}
			cmd := strings.Join(fields[1:], " ")
			out = append(out, AppInfo{p, cmd})
		}
	}
	return out, nil
}

func (s *Sandbox) ScreenshotBytes(ctx context.Context) ([]byte, error) {
	path := fmt.Sprintf("/tmp/novita-screenshot-%d.png", time.Now().UnixNano())
	if _, err := s.Commands.Run(ctx, "scrot --pointer "+shellQuote(path), nil); err != nil {
		return nil, err
	}
	defer s.Files.Remove(context.Background(), path, nil)
	return s.Files.Read(ctx, path, nil)
}

func (s *Sandbox) Open(ctx context.Context, fileOrURL string) (*core.CommandHandle, error) {
	return s.Commands.Start(ctx, "xdg-open "+shellQuote(fileOrURL), &core.CommandStartOpts{Background: true})
}

func (s *Sandbox) CurrentWindowID(ctx context.Context) (string, error) {
	result, err := s.Commands.Run(ctx, "xdotool getwindowfocus", nil)
	return strings.TrimSpace(result.Stdout), err
}

func (s *Sandbox) ApplicationWindows(ctx context.Context, application string) ([]string, error) {
	result, err := s.Commands.Run(ctx, "xdotool search --onlyvisible --class "+shellQuote(application), nil)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(result.Stdout), nil
}

func (s *Sandbox) WindowTitle(ctx context.Context, windowID string) (string, error) {
	result, err := s.Commands.Run(ctx, "xdotool getwindowname "+shellQuote(windowID), nil)
	return strings.TrimSpace(result.Stdout), err
}

func (s *Sandbox) Windows(ctx context.Context) ([]WindowInfo, error) {
	result, err := s.Commands.Run(ctx, "xdotool search --onlyvisible --name .", nil)
	if err != nil {
		return nil, err
	}
	ids := nonEmptyLines(result.Stdout)
	windows := make([]WindowInfo, 0, len(ids))
	for _, id := range ids {
		title, titleErr := s.WindowTitle(ctx, id)
		if titleErr != nil {
			return nil, titleErr
		}
		pidResult, pidErr := s.Commands.Run(ctx, "xdotool getwindowpid "+shellQuote(id), nil)
		if pidErr != nil {
			return nil, pidErr
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(pidResult.Stdout))
		windows = append(windows, WindowInfo{ID: id, Title: title, PID: pid})
	}
	return windows, nil
}

func (s *Sandbox) FocusWindow(ctx context.Context, windowID string) error {
	_, err := s.Commands.Run(ctx, "xdotool windowactivate --sync "+shellQuote(windowID), nil)
	return err
}
func (s *Sandbox) Launch(ctx context.Context, application, uri string) (*App, error) {
	command := "gtk-launch " + shellQuote(application)
	if uri != "" {
		command += " " + shellQuote(uri)
	}
	handle, err := s.Commands.Start(ctx, command, &core.CommandStartOpts{Background: true})
	if err != nil {
		return nil, err
	}
	handle.Disconnect()
	return &App{PID: handle.PID(), Command: command, desktop: s}, nil
}

func (a *App) Terminate(ctx context.Context) error {
	if a == nil || a.desktop == nil {
		return fmt.Errorf("desktop app is not initialized")
	}
	return a.desktop.TerminateApp(ctx, a.PID)
}
func (a *App) Focus(ctx context.Context) error {
	if a == nil || a.desktop == nil {
		return fmt.Errorf("desktop app is not initialized")
	}
	return a.desktop.FocusApp(ctx, a.PID)
}

func newVNCServer(s *Sandbox) *VNCServer {
	return &VNCServer{desktop: s, vncPort: 5900, noVNCPort: 6080}
}

func (v *VNCServer) Start(ctx context.Context, opts VNCStartOptions) error {
	if v.running {
		return fmt.Errorf("desktop stream is already running")
	}
	if opts.VNCPort > 0 {
		v.vncPort = opts.VNCPort
	}
	if opts.NoVNCPort > 0 {
		v.noVNCPort = opts.NoVNCPort
	}
	pwdFlag := "-nopw"
	if opts.RequireAuth {
		buffer := make([]byte, 8)
		if _, err := rand.Read(buffer); err != nil {
			return err
		}
		v.password = hex.EncodeToString(buffer)
		if _, err := v.desktop.Commands.Run(ctx, "mkdir -p ~/.vnc && x11vnc -storepasswd "+shellQuote(v.password)+" ~/.vnc/passwd", nil); err != nil {
			return err
		}
		pwdFlag = "-usepw"
	} else {
		v.password = ""
	}
	command := fmt.Sprintf("x11vnc -bg -display %s -forever -wait 50 -shared -rfbport %d %s 2>/tmp/x11vnc_stderr.log", shellQuote(v.desktop.Display), v.vncPort, pwdFlag)
	if opts.WindowID != "" {
		command += " -id " + shellQuote(opts.WindowID)
	}
	if _, err := v.desktop.Commands.Run(ctx, command, nil); err != nil {
		return err
	}
	novnc := fmt.Sprintf("cd /opt/noVNC/utils && ./novnc_proxy --vnc localhost:%d --listen %d --web /opt/noVNC >/tmp/novnc.log 2>&1", v.vncPort, v.noVNCPort)
	handle, err := v.desktop.Commands.Start(ctx, novnc, &core.CommandStartOpts{Background: true})
	if err != nil {
		return err
	}
	defer handle.Disconnect()
	v.handle = handle
	if err := v.desktop.waitFor(ctx, fmt.Sprintf("netstat -tuln | grep ':%d '", v.noVNCPort), 10*time.Second); err != nil {
		_ = handle.Kill(context.Background())
		return err
	}
	v.running = true
	return nil
}

func (v *VNCServer) Stop(ctx context.Context) error {
	if _, err := v.desktop.Commands.Run(ctx, "pkill x11vnc || true", nil); err != nil {
		return err
	}
	if v.handle != nil {
		_ = v.handle.Kill(ctx)
		v.handle = nil
	}
	v.running = false
	return nil
}

func (v *VNCServer) URL(opts VNCURLOptions) (string, error) {
	if !v.running {
		return "", fmt.Errorf("desktop stream is not running")
	}
	target, err := url.Parse(v.desktop.PortURL(v.noVNCPort) + "/vnc.html")
	if err != nil {
		return "", err
	}
	query := target.Query()
	if opts.AutoConnect {
		query.Set("autoconnect", "true")
	}
	if opts.ViewOnly {
		query.Set("view_only", "true")
	}
	if opts.Resize == "" {
		opts.Resize = "scale"
	}
	if opts.Resize != "off" && opts.Resize != "scale" && opts.Resize != "remote" {
		return "", fmt.Errorf("invalid resize mode %q", opts.Resize)
	}
	query.Set("resize", opts.Resize)
	if opts.AuthKey != "" {
		query.Set("password", opts.AuthKey)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (v *VNCServer) AuthKey() (string, error) {
	if v.password == "" {
		return "", fmt.Errorf("VNC authentication is not enabled")
	}
	return v.password, nil
}

func (s *Sandbox) waitFor(ctx context.Context, command string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if result, err := s.Commands.Run(ctx, command, nil); err == nil && strings.TrimSpace(result.Stdout) != "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return &core.TimeoutError{Operation: command, Duration: timeout.String()}
		case <-ticker.C:
		}
	}
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

var specialKeys = map[string]string{
	"alt": "Alt_L", "alt_left": "Alt_L", "alt_right": "Alt_R",
	"backspace": "BackSpace", "break": "Pause", "caps_lock": "Caps_Lock",
	"cmd": "Super_L", "command": "Super_L", "control": "Control_L",
	"control_left": "Control_L", "control_right": "Control_R", "ctrl": "Control_L",
	"del": "Delete", "delete": "Delete", "down": "Down", "end": "End",
	"enter": "Return", "esc": "Escape", "escape": "Escape", "home": "Home",
	"insert": "Insert", "left": "Left", "menu": "Menu", "meta": "Meta_L",
	"num_lock": "Num_Lock", "page_down": "Page_Down", "page_up": "Page_Up",
	"pause": "Pause", "print": "Print", "right": "Right", "scroll_lock": "Scroll_Lock",
	"shift": "Shift_L", "shift_left": "Shift_L", "shift_right": "Shift_R",
	"space": "space", "super": "Super_L", "super_left": "Super_L", "super_right": "Super_R",
	"tab": "Tab", "up": "Up", "win": "Super_L", "windows": "Super_L",
}

func mapKey(key string) string {
	lower := strings.ToLower(key)
	if mapped, ok := specialKeys[lower]; ok {
		return mapped
	}
	if len(lower) > 1 && lower[0] == 'f' {
		if number, err := strconv.Atoi(lower[1:]); err == nil && number >= 1 && number <= 12 {
			return strings.ToUpper(lower)
		}
	}
	return lower
}
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }

// CreateDesktopSandbox creates a sandbox suited to desktop automation.
func CreateDesktopSandbox(ctx context.Context, opts *core.SandboxOpts) (*Sandbox, error) {
	return Create(ctx, CreateOptions{Sandbox: opts})
}

func Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	sandboxOpts := &core.SandboxOpts{}
	if opts.Sandbox != nil {
		*sandboxOpts = *opts.Sandbox
	}
	if sandboxOpts.Template == "" && sandboxOpts.Image == "" {
		sandboxOpts.Template = "desktop"
	}
	if opts.Display == "" {
		opts.Display = ":0"
	}
	if opts.Resolution.Width == 0 {
		opts.Resolution.Width = 1024
	}
	if opts.Resolution.Height == 0 {
		opts.Resolution.Height = 768
	}
	if opts.DPI == 0 {
		opts.DPI = 96
	}
	if sandboxOpts.EnvVars == nil {
		sandboxOpts.EnvVars = map[string]string{}
	} else {
		copyEnv := make(map[string]string, len(sandboxOpts.EnvVars)+1)
		for key, value := range sandboxOpts.EnvVars {
			copyEnv[key] = value
		}
		sandboxOpts.EnvVars = copyEnv
	}
	sandboxOpts.EnvVars["DISPLAY"] = opts.Display
	inner, err := core.CreateSandbox(ctx, sandboxOpts)
	if err != nil {
		return nil, err
	}
	desktop := &Sandbox{Sandbox: inner, Display: opts.Display}
	desktop.VNC = newVNCServer(desktop)
	command := fmt.Sprintf("Xvfb %s -ac -screen 0 %dx%dx24 -retro -dpi %d -nolisten tcp -nolisten unix", shellQuote(opts.Display), opts.Resolution.Width, opts.Resolution.Height, opts.DPI)
	displayHandle, err := desktop.Commands.Start(ctx, command, &core.CommandStartOpts{Background: true})
	if err != nil {
		return nil, err
	}
	defer displayHandle.Disconnect()
	if err := desktop.waitFor(ctx, "xdpyinfo -display "+shellQuote(opts.Display)+" && echo ready", 10*time.Second); err != nil {
		return nil, err
	}
	handle, err := desktop.Commands.Start(ctx, "startxfce4", &core.CommandStartOpts{Background: true})
	if err != nil {
		return nil, err
	}
	desktop.xfcePID = handle.PID()
	handle.Disconnect()
	return desktop, nil
}

// ConnectDesktopSandbox attaches to an existing sandbox.
func ConnectDesktopSandbox(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*Sandbox, error) {
	inner, err := core.Connect(ctx, sandboxID, opts)
	if err != nil {
		return nil, err
	}
	desktop := &Sandbox{Sandbox: inner, Display: ":0"}
	desktop.VNC = newVNCServer(desktop)
	return desktop, nil
}
