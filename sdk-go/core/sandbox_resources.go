package core

import "math"

const (
	// MIBPerGiB is the number of MiB in one GiB, the unit the API uses for
	// memory and disk totals.
	MIBPerGiB = 1024
	// ResourceCreateTimeoutMS is the default request timeout for a
	// resource-aware sandbox creation. The backend allows six minutes.
	ResourceCreateTimeoutMS = 360_000
)

// SandboxResourceTargets holds absolute resource totals for a sandbox, not
// increments.
//
// CPU is a core count. Memory and disk are user-facing GiB values that the SDK
// converts to the MiB values the API expects; a changed total must be an exact
// number of MiB, which allows fractional GiB values that match a template's
// size. Zero fields are left unchanged, so at least one field must be set.
type SandboxResourceTargets struct {
	// CPU is the total virtual CPU count. It must be positive.
	CPU int
	// MemoryGiB is the total memory in GiB.
	MemoryGiB float64
	// DiskGiB is the total disk size in GiB.
	DiskGiB float64
}

// SandboxResizeItem is the outcome of one resource in a resize request.
//
// Totals are pointers because the API may omit them, for example when the node
// did not confirm the new value; a missing total is nil rather than zero.
type SandboxResizeItem struct {
	// Status is one of "applied", "skipped", "failed" or "unknown".
	Status string
	// Requested is the requested total: cores for CPU, GiB for memory and disk.
	Requested *float64
	// Previous is the total before the call in the same unit as Requested.
	Previous *float64
	// Current is the total after the call in the same unit as Requested.
	Current *float64
	// Unit is "cores" for CPU and "GiB" for memory and disk.
	Unit string
	// Code is the HTTP status this resource would have returned on its own.
	Code int
	// Reason is a reason code when the resource was not applied.
	Reason string
	// Message is a human-readable reason when the resource was not applied.
	Message string
}

// SandboxResizeResult reports the outcome of each requested resource. Only
// resources included in the request are present.
type SandboxResizeResult struct {
	CPU    *SandboxResizeItem
	Memory *SandboxResizeItem
	Disk   *SandboxResizeItem
}

// toAPI validates the public GiB resource totals and returns the MiB request
// body.
func (t SandboxResourceTargets) toAPI() (map[string]int64, error) {
	body := map[string]int64{}
	if t.CPU != 0 {
		if t.CPU < 0 {
			return nil, &InvalidArgumentError{
				Argument: "resources",
				Reason:   "resource target cpu must be a positive whole number",
			}
		}
		body["vcpu"] = int64(t.CPU)
	}
	if t.MemoryGiB != 0 {
		mib, err := gibToMib(t.MemoryGiB, "memoryGiB")
		if err != nil {
			return nil, err
		}
		body["memory_mb"] = mib
	}
	if t.DiskGiB != 0 {
		mib, err := gibToMib(t.DiskGiB, "diskGiB")
		if err != nil {
			return nil, err
		}
		body["disk_size_mb"] = mib
	}
	if len(body) == 0 {
		return nil, &InvalidArgumentError{
			Argument: "resources",
			Reason:   "at least one of cpu, memoryGiB or diskGiB is required",
		}
	}
	return body, nil
}

// gibToMib converts a positive GiB value to MiB. Values that are not an exact
// number of MiB are rejected instead of being rounded silently.
func gibToMib(value float64, name string) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, &InvalidArgumentError{
			Argument: "resources",
			Reason:   "resource target " + name + " must be a positive number",
		}
	}
	mib := value * MIBPerGiB
	if mib != math.Trunc(mib) {
		return 0, &InvalidArgumentError{
			Argument: "resources",
			Reason:   "resource target " + name + " must be an exact number of MiB; use a multiple of 1/1024 GiB",
		}
	}
	if mib >= math.MaxInt64 {
		return 0, &InvalidArgumentError{
			Argument: "resources",
			Reason:   "resource target " + name + " is too large",
		}
	}
	return int64(mib), nil
}

// sandboxResizeItemWire mirrors the MiB values the resize endpoint returns.
type sandboxResizeItemWire struct {
	Status    string   `json:"status"`
	Requested *float64 `json:"requested"`
	Previous  *float64 `json:"previous"`
	Current   *float64 `json:"current"`
	Code      int      `json:"code"`
	Reason    string   `json:"reason"`
	Message   string   `json:"message"`
}

type sandboxResizeWire struct {
	CPU    *sandboxResizeItemWire `json:"cpu"`
	Memory *sandboxResizeItemWire `json:"memory"`
	Disk   *sandboxResizeItemWire `json:"disk"`
}

// toResult maps the MiB values returned by the API to the GiB values users
// see. CPU counts are returned unchanged.
func (w sandboxResizeWire) toResult() *SandboxResizeResult {
	result := &SandboxResizeResult{
		CPU:    w.CPU.toItem(false),
		Memory: w.Memory.toItem(true),
		Disk:   w.Disk.toItem(true),
	}
	return result
}

func (w *sandboxResizeItemWire) toItem(toGiB bool) *SandboxResizeItem {
	if w == nil {
		return nil
	}
	item := &SandboxResizeItem{
		Status:    w.Status,
		Requested: w.Requested,
		Previous:  w.Previous,
		Current:   w.Current,
		Unit:      "cores",
		Code:      w.Code,
		Reason:    w.Reason,
		Message:   w.Message,
	}
	if toGiB {
		item.Requested = mibToGib(w.Requested)
		item.Previous = mibToGib(w.Previous)
		item.Current = mibToGib(w.Current)
		item.Unit = "GiB"
	}
	return item
}

// validateFor checks that every requested resource has an outcome item. A
// partial 207 result is valid, but a missing item for a requested resource is a
// malformed response rather than success.
func (r *SandboxResizeResult) validateFor(targets SandboxResourceTargets) error {
	if targets.CPU != 0 && r.CPU == nil {
		return &SandboxError{Message: "resize response is missing the cpu result"}
	}
	if targets.MemoryGiB != 0 && r.Memory == nil {
		return &SandboxError{Message: "resize response is missing the memory result"}
	}
	if targets.DiskGiB != 0 && r.Disk == nil {
		return &SandboxError{Message: "resize response is missing the disk result"}
	}
	return nil
}

// mibToGib converts an optional MiB total to GiB, preserving a missing value.
func mibToGib(value *float64) *float64 {
	if value == nil {
		return nil
	}
	gib := *value / MIBPerGiB
	return &gib
}
