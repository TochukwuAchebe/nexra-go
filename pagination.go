package nexra

import (
	"context"
	"iter"
)

// Page is one page of a cursor-paged list. NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Paginate follows cursors until the last page. fetch receives the cursor
// ("" for the first page). Most Nexra lists return a capped array and need no
// paging; governance lists return items and nextCursor.
//
//	pages := nexra.Paginate(ctx, func(ctx context.Context, cursor string) (nexra.Page[nexraapi.EvidenceEntry], error) {
//		request := client.GovernanceAPI.ListEvidence(ctx)
//		if cursor != "" {
//			request = request.Cursor(cursor)
//		}
//		page, response, err := request.Execute()
//		if err != nil {
//			return nexra.Page[nexraapi.EvidenceEntry]{}, nexra.ParseError(response, err)
//		}
//		return nexra.Page[nexraapi.EvidenceEntry]{Items: page.Items, NextCursor: page.GetNextCursor()}, nil
//	})
//	for entry, err := range pages { ... }
func Paginate[T any](ctx context.Context, fetch func(ctx context.Context, cursor string) (Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := ""
		seen := map[string]bool{}
		for {
			if err := ctx.Err(); err != nil {
				var zero T
				yield(zero, err)
				return
			}
			page, err := fetch(ctx, cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range page.Items {
				if !yield(item, nil) {
					return
				}
			}
			if page.NextCursor == "" || seen[page.NextCursor] {
				return
			}
			seen[page.NextCursor] = true
			cursor = page.NextCursor
		}
	}
}

// Collect gathers up to limit items (0 for all) from Paginate.
func Collect[T any](items iter.Seq2[T, error], limit int) ([]T, error) {
	var out []T
	for item, err := range items {
		if err != nil {
			return out, err
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, item)
	}
	return out, nil
}
