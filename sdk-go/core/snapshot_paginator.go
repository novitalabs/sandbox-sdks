package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type SnapshotPaginator struct {
	client *Client
	opts   SnapshotListOptions
	next   string
	done   bool
}

func (c *Client) SnapshotPaginator(opts SnapshotListOptions) *SnapshotPaginator {
	return &SnapshotPaginator{client: c, opts: opts}
}

func (p *SnapshotPaginator) HasNext() bool { return !p.done }

func (p *SnapshotPaginator) Next(ctx context.Context) ([]SnapshotInfo, error) {
	if p.done {
		return []SnapshotInfo{}, nil
	}
	if p.client.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.ListSnapshots", Reason: "requires a modern domain"}
	}
	q := url.Values{}
	if p.opts.SourceSandboxID != "" {
		q.Set("sandboxID", p.opts.SourceSandboxID)
	}
	if p.opts.Limit > 0 {
		q.Set("limit", fmt.Sprint(p.opts.Limit))
	}
	next := p.next
	if next == "" {
		next = p.opts.Cursor
	}
	if next != "" {
		q.Set("nextToken", next)
	}
	res, err := p.client.do(ctx, http.MethodGet, "/snapshots", q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := p.client.checkResponse(res); err != nil {
		return nil, err
	}
	var items []SnapshotInfo
	if err := json.NewDecoder(res.Body).Decode(&items); err != nil {
		return nil, err
	}
	p.next = res.Header.Get("X-Next-Token")
	p.done = p.next == ""
	return items, nil
}
