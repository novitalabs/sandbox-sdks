package core

import "context"

type TemplatePaginator struct {
	client *TemplateClient
	opts   TemplateListOptions
	done   bool
}

func (t *TemplateClient) Paginator(opts TemplateListOptions) *TemplatePaginator {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	return &TemplatePaginator{client: t, opts: opts}
}

func (p *TemplatePaginator) HasNext() bool { return !p.done }

func (p *TemplatePaginator) Next(ctx context.Context) ([]TemplateInfo, error) {
	if p.done {
		return []TemplateInfo{}, nil
	}
	page, err := p.client.List(ctx, &p.opts)
	if err != nil {
		return nil, err
	}
	current := page.Page
	if current == 0 {
		current = p.opts.Page
	}
	if page.TotalPages > 0 {
		p.done = current >= page.TotalPages
	} else {
		p.done = len(page.Templates) == 0 || (p.opts.Limit > 0 && len(page.Templates) < p.opts.Limit)
	}
	p.opts.Page = current + 1
	return page.Templates, nil
}
