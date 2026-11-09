package core

import "context"

// Paginator incrementally retrieves pages of API resources.
type Paginator[T any] interface {
	Next(context.Context) ([]T, error)
	HasNext() bool
}

type slicePaginator[T any] struct {
	items    []T
	pageSize int
	index    int
}

func newSlicePaginator[T any](items []T, pageSize int) *slicePaginator[T] {
	if pageSize <= 0 {
		pageSize = 20
	}
	return &slicePaginator[T]{items: items, pageSize: pageSize}
}
func (p *slicePaginator[T]) Next(ctx context.Context) ([]T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.index >= len(p.items) {
		return []T{}, nil
	}
	end := p.index + p.pageSize
	if end > len(p.items) {
		end = len(p.items)
	}
	out := p.items[p.index:end]
	p.index = end
	return out, nil
}
func (p *slicePaginator[T]) HasNext() bool { return p.index < len(p.items) }
