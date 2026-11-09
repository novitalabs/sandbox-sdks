package core

import (
	"encoding/json"
	"strconv"
	"time"
)

// TemplateBuildInfo is one entry of a template's builds array.
type TemplateBuildInfo struct {
	BuildID string `json:"buildID,omitempty"`
	Status  string `json:"status,omitempty"`
}

type TemplateInfo struct {
	TemplateID    string            `json:"templateID"`
	BuildID       string            `json:"buildID,omitempty"`
	Name          string            `json:"name,omitempty"`
	Alias         string            `json:"alias,omitempty"`
	BuildStatus   string            `json:"buildStatus,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Aliases       []string          `json:"aliases,omitempty"`
	Names         []string          `json:"names,omitempty"`
	TemplateType  string            `json:"templateType,omitempty"`
	BuildCount    int               `json:"buildCount,omitempty"`
	CPUCount      int               `json:"cpuCount,omitempty"`
	MemoryMB      int               `json:"memoryMB,omitempty"`
	DiskSizeMB    int               `json:"diskSizeMB,omitempty"`
	SpawnCount    int               `json:"spawnCount,omitempty"`
	EnvdVersion   string            `json:"envdVersion,omitempty"`
	Public        bool              `json:"public,omitempty"`
	CreatedAt     *time.Time        `json:"createdAt,omitempty"`
	UpdatedAt     *time.Time        `json:"updatedAt,omitempty"`
	LastSpawnedAt *time.Time        `json:"lastSpawnedAt,omitempty"`
	CreatedBy     map[string]any    `json:"createdBy,omitempty"`
	// Builds is the template's build history; Template.Get derives the
	// effective buildID/buildStatus from builds[0], matching JS/Python.
	Builds []TemplateBuildInfo `json:"builds,omitempty"`
}

// SandboxState represents the state of a sandbox
type SandboxState string

const (
	SandboxStateRunning SandboxState = "running"
	SandboxStatePaused  SandboxState = "paused"
)

// SandboxOnTimeout represents the action taken when a sandbox times out
type SandboxOnTimeout string

const (
	OnTimeoutKill  SandboxOnTimeout = "kill"
	OnTimeoutPause SandboxOnTimeout = "pause"
)

// FileType represents the type of a file system entry
type FileType string

const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "dir"
	FileTypeSymlink FileType = "symlink"
)

// SandboxInfo contains complete information about a sandbox
type SandboxInfo struct {
	SandboxID           string                `json:"sandboxID"`
	TemplateID          string                `json:"templateID"`
	Alias               string                `json:"alias,omitempty"`
	ClientID            string                `json:"clientID"` // deprecated
	State               SandboxState          `json:"state"`
	StartedAt           time.Time             `json:"startedAt"`
	EndAt               time.Time             `json:"endAt"`
	EnvdVersion         string                `json:"envdVersion"`
	EnvdAccessToken     string                `json:"envdAccessToken,omitempty"`
	TrafficAccessToken  string                `json:"trafficAccessToken,omitempty"`
	Domain              *string               `json:"domain"`
	AllowInternetAccess *bool                 `json:"allowInternetAccess"`
	CPUCount            int32                 `json:"cpuCount"`
	MemoryMB            int32                 `json:"memoryMB"`
	DiskSizeMB          int32                 `json:"diskSizeMB"`
	Metadata            map[string]string     `json:"metadata,omitempty"`
	Network             *SandboxNetworkConfig `json:"network,omitempty"`
	Lifecycle           *SandboxLifecycle     `json:"lifecycle,omitempty"`
	VolumeMounts        []SandboxVolumeMount  `json:"volumeMounts,omitempty"`
}

// SandboxNetworkConfig contains network configuration for a sandbox
type SandboxNetworkConfig struct {
	// AllowPublicTraffic is retained for source compatibility. Use
	// AllowPublicTrafficOption for explicit false; zero defaults to public.
	AllowPublicTraffic bool `json:"allowPublicTraffic"`
	// AllowPublicTrafficOption preserves explicit false. Nil defaults to true.
	AllowPublicTrafficOption *bool    `json:"-"`
	AllowOut                 []string `json:"allowOut,omitempty"`
	DenyOut                  []string `json:"denyOut,omitempty"`
	MaskRequestHost          string   `json:"maskRequestHost,omitempty"`
}

// SandboxLifecycle contains lifecycle policy for a sandbox
type SandboxLifecycle struct {
	AutoResume bool             `json:"autoResume"`
	OnTimeout  SandboxOnTimeout `json:"onTimeout"`
}

// SandboxVolumeMount represents a volume mount in a sandbox
type SandboxVolumeMount struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// SandboxMetrics contains metrics about a sandbox
type SandboxMetrics struct {
	Timestamp     time.Time `json:"timestamp"`
	TimestampUnix int64     `json:"timestampUnix,omitempty"`
	CPUUsedPct    float64   `json:"cpuUsedPct"`
	CPUCount      int32     `json:"cpuCount"`
	MemUsed       int64     `json:"memUsed"`
	MemTotal      int64     `json:"memTotal"`
	DiskUsed      int64     `json:"diskUsed"`
	DiskTotal     int64     `json:"diskTotal"`
}

func (m *SandboxMetrics) UnmarshalJSON(data []byte) error {
	var raw struct {
		Timestamp     json.RawMessage `json:"timestamp"`
		TimestampUnix int64           `json:"timestampUnix"`
		CPUUsedPct    float64         `json:"cpuUsedPct"`
		CPUCount      int32           `json:"cpuCount"`
		MemUsed       int64           `json:"memUsed"`
		MemTotal      int64           `json:"memTotal"`
		MemUsedMiB    float64         `json:"memUsedMiB"`
		MemTotalMiB   float64         `json:"memTotalMiB"`
		DiskUsed      *int64          `json:"diskUsed"`
		DiskTotal     *int64          `json:"diskTotal"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.TimestampUnix = raw.TimestampUnix
	m.CPUUsedPct = raw.CPUUsedPct
	m.CPUCount = raw.CPUCount
	m.MemUsed = raw.MemUsed
	m.MemTotal = raw.MemTotal
	if m.MemUsed == 0 && raw.MemUsedMiB != 0 {
		m.MemUsed = int64(raw.MemUsedMiB * 1024 * 1024)
	}
	if m.MemTotal == 0 && raw.MemTotalMiB != 0 {
		m.MemTotal = int64(raw.MemTotalMiB * 1024 * 1024)
	}
	m.DiskUsed, m.DiskTotal = -1, -1
	if raw.DiskUsed != nil {
		m.DiskUsed = *raw.DiskUsed
	}
	if raw.DiskTotal != nil {
		m.DiskTotal = *raw.DiskTotal
	}
	if raw.TimestampUnix != 0 {
		m.Timestamp = time.Unix(raw.TimestampUnix, 0)
	}
	if len(raw.Timestamp) != 0 && string(raw.Timestamp) != "null" {
		var value string
		if json.Unmarshal(raw.Timestamp, &value) == nil {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				m.Timestamp = parsed
			} else if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
				m.Timestamp = time.Unix(seconds, 0)
			}
		} else {
			var seconds float64
			if json.Unmarshal(raw.Timestamp, &seconds) == nil {
				// Legacy timestamps are milliseconds; modern numeric timestamps are seconds.
				if seconds > 1e11 {
					m.Timestamp = time.UnixMilli(int64(seconds))
				} else {
					m.Timestamp = time.Unix(int64(seconds), 0)
				}
			}
		}
	}
	return nil
}

// SandboxQuota contains quota information for a team
type SandboxQuota struct {
	Limit SandboxQuotaLimit `json:"limit"`
	Usage SandboxQuotaUsage `json:"usage"`
}

// SandboxQuotaLimit contains quota limits
type SandboxQuotaLimit struct {
	ConcurrentInstances int64  `json:"concurrentInstances"`
	ConcurrentVcpu      *int64 `json:"concurrentVcpu"`
	ConcurrentRamMb     *int64 `json:"concurrentRamMb"`
	MaxLengthHours      int64  `json:"maxLengthHours"`
	DiskMb              int64  `json:"diskMb"`
	MaxVcpu             int64  `json:"maxVcpu"`
	MaxRamMb            int64  `json:"maxRamMb"`
}

// SandboxQuotaUsage contains current quota usage
type SandboxQuotaUsage struct {
	ConcurrentInstances int64 `json:"concurrentInstances"`
	ConcurrentVcpu      int64 `json:"concurrentVcpu"`
	ConcurrentRamMb     int64 `json:"concurrentRamMb"`
}

// SandboxEventItem represents an event in sandbox lifecycle
type SandboxEventItem struct {
	EventID      string `json:"eventID"`
	RecordAt     int64  `json:"recordAt"`
	TemplateID   string `json:"templateID"`
	TemplateName string `json:"templateName"`
	SandboxID    string `json:"sandboxID"`
	EventName    string `json:"eventName"`
	State        string `json:"state"`
	ErrorMessage string `json:"errorMsg"`
	StatusCode   int    `json:"statusCode"`
}

// SandboxEventsResult contains paginated events
type SandboxEventsResult struct {
	Items   []SandboxEventItem `json:"items"`
	Total   int                `json:"total"`
	HasMore bool               `json:"hasMore"`
}

// CloneSandboxRequest contains options for cloning a sandbox
type CloneSandboxRequest struct {
	Count   int32  `json:"count"`
	Strict  bool   `json:"strict"`
	NodeID  string `json:"nodeID,omitempty"`
	Timeout int32  `json:"timeout,omitempty"`
}

type CloneOptions struct {
	Count   int32
	Strict  bool
	NodeID  string
	Timeout time.Duration
}
type SnapshotListOptions struct {
	SourceSandboxID string
	Limit           int
	Cursor          string
}
type SandboxListOptions struct {
	State     SandboxState // Deprecated: use States.
	States    []SandboxState
	Metadata  map[string]string
	Page      int // Deprecated: the API uses NextToken.
	Limit     int
	Cursor    string // Deprecated: use NextToken.
	NextToken string
}
type ResetOptions struct {
	Resume             *bool
	Timeout            time.Duration
	Secure             *bool
	AllowPublicTraffic *bool
	Network            *SandboxNetworkConfig
}

type PauseOptions struct {
	// Sync waits for the legacy pause operation to complete before returning.
	// Modern domains already use the synchronous pause endpoint and ignore it.
	Sync bool
}

type SandboxMetricsOptions struct {
	Start *time.Time
	End   *time.Time
}

type SandboxEventsOptions struct {
	StartTime  *int64
	EndTime    *int64
	SandboxID  string
	TemplateID string
	Events     string
	State      string
	OrderAsc   *bool
	Offset     *int
	Limit      *int
}

type VolumeMountOptions struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type NetworkUpdateOptions struct {
	AllowOut []string
	DenyOut  []string
}

// CloneSandboxResponse contains the result of a clone operation
type CloneSandboxResponse struct {
	Sandboxes          []SandboxInfo `json:"sandboxes"`
	Count              int32         `json:"count"`
	SnapshotTemplateId string        `json:"snapshotTemplateId"`
}

// SnapshotInfo contains information about a snapshot
type SnapshotInfo struct {
	SnapshotID string   `json:"snapshotID"`
	Names      []string `json:"names"`
}

// EntryInfo contains information about a file or directory
type EntryInfo struct {
	Name          string     `json:"name"`
	Path          string     `json:"path"`
	Type          FileType   `json:"type"`
	Size          int64      `json:"size"`
	Mode          int32      `json:"mode"`
	Permissions   string     `json:"permissions"`
	Owner         string     `json:"owner"`
	Group         string     `json:"group"`
	ModifiedTime  *time.Time `json:"modifiedTime,omitempty"`
	SymlinkTarget *string    `json:"symlinkTarget,omitempty"`
}

// WriteInfo contains information about a written filesystem object.
//
// EntryInfo embeds this, mirroring the JS/Python SDKs where EntryInfo extends
// WriteInfo with size and ownership details.
type WriteInfo struct {
	// Name of the filesystem object.
	Name string `json:"name"`
	// Type of the filesystem object, when known.
	Type FileType `json:"type,omitempty"`
	// Path to the filesystem object.
	Path string `json:"path"`
	// Bytes written; populated for uploads that report a size.
	Bytes int64 `json:"bytes,omitempty"`
}

// CommandResult contains the result of a command execution
type CommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// ProcessInfo contains information about a running process
type ProcessInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	User    string `json:"user,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

// GitStatus contains git repository status
type GitStatus struct {
	Files          []GitFileStatus `json:"files"`
	Branch         string          `json:"branch"`
	Ahead          int             `json:"ahead"`
	Behind         int             `json:"behind"`
	HasUncommitted bool            `json:"hasUncommitted"`
}

// GitFileStatus represents the status of a single file
type GitFileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"` // e.g., "modified", "added", "deleted"
}

// GitBranches contains information about git branches
type GitBranches struct {
	Current string   `json:"current"`
	Local   []string `json:"local"`
	Remote  []string `json:"remote"`
}

// Username represents a user specification for command execution.
//
// The JS/Python SDKs model this as a plain string; Name is the equivalent field.
type Username struct {
	Name string
}

type TemplateTag struct {
	Tag       string    `json:"tag"`
	BuildID   string    `json:"buildID"`
	CreatedAt time.Time `json:"createdAt"`
}
type TemplateTagInfo struct {
	BuildID string   `json:"buildID"`
	Tags    []string `json:"tags"`
}

// MarshalJSON treats an unspecified ingress policy as public, like Python/JS.
func (n SandboxNetworkConfig) MarshalJSON() ([]byte, error) {
	body := map[string]any{"allowPublicTraffic": true}
	if n.AllowPublicTrafficOption != nil {
		body["allowPublicTraffic"] = *n.AllowPublicTrafficOption
	}
	if n.AllowOut != nil {
		body["allowOut"] = n.AllowOut
	}
	if n.DenyOut != nil {
		body["denyOut"] = n.DenyOut
	}
	if n.MaskRequestHost != "" {
		body["maskRequestHost"] = n.MaskRequestHost
	}
	return json.Marshal(body)
}
func (n *SandboxNetworkConfig) UnmarshalJSON(data []byte) error {
	type plain SandboxNetworkConfig
	var value struct {
		*plain
		Public *bool `json:"allowPublicTraffic"`
	}
	value.plain = (*plain)(n)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	n.AllowPublicTrafficOption = value.Public
	n.AllowPublicTraffic = value.Public == nil || *value.Public
	return nil
}
