package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type SandboxPaginator struct {
	client *Client
	opts   SandboxListOptions
	next   string
	first  bool
	done   bool
}

func (c *Client) SandboxPaginator(opts SandboxListOptions) *SandboxPaginator {
	return &SandboxPaginator{client: c, opts: opts, first: true}
}
func (p *SandboxPaginator) HasNext() bool { return !p.done }
func (p *SandboxPaginator) Next(ctx context.Context) ([]SandboxInfo, error) {
	if p.done {
		return []SandboxInfo{}, nil
	}
	q := url.Values{}
	states := append([]SandboxState(nil), p.opts.States...)
	if len(states) == 0 && p.opts.State != "" {
		states = append(states, p.opts.State)
	}
	if len(states) > 0 {
		values := make([]string, len(states))
		for i, state := range states {
			values[i] = string(state)
		}
		q.Set("state", strings.Join(values, ","))
	}
	if p.opts.Limit > 0 {
		q.Set("limit", fmt.Sprint(p.opts.Limit))
	}
	if len(p.opts.Metadata) > 0 {
		metadata := url.Values{}
		for key, value := range p.opts.Metadata {
			metadata.Set(key, value)
		}
		q.Set("metadata", metadata.Encode())
	}
	next := p.next
	if next == "" {
		next = p.opts.NextToken
	}
	if next == "" {
		next = p.opts.Cursor
	}
	if next != "" {
		q.Set("nextToken", next)
	}
	items, next, err := p.client.listSandboxesPage(ctx, q.Encode())
	if err != nil {
		return nil, err
	}
	p.next = next
	p.first = false
	if next == "" {
		p.done = true
	}
	return items, nil
}
