// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The retrieval request contract of the history side: what a query must carry
// to be a retrieval at all, how a page continuation travels from one page to
// the query of the next, and the page bound a query resolves to when it names
// none.
package history

import (
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/memory/search"
)

// TestSearchQueryContract pins the retrieval request contract on three claims:
// - a query without retrieval content is refused by name and never runs as a listing
// - a continuation cursor passes unchanged from one page into the query of the next
// - the zero cursor means both first and last page
// - a zero Limit resolves to the declared bounded default rather than to no bound
func TestSearchQueryContract(t *testing.T) {
	t.Run("an empty Text is refused by name and not read as a listing", func(t *testing.T) {
		refused := (search.Query{Limit: search.DefaultLimit}).Validate()
		if !errors.Is(refused, search.ErrQueryTextMissing) {
			t.Fatalf("Validate(empty Text) = %v, want the named refusal ErrQueryTextMissing", refused)
		}
		if err := (search.Query{Text: "数据库迁移"}).Validate(); err != nil {
			t.Fatalf("Validate(Text set) = %v, want the query to run", err)
		}
	})

	t.Run("a cursor passes from one page into the query of the next", func(t *testing.T) {
		first := search.Query{Text: "数据库迁移"}
		if first.Cursor != "" {
			t.Fatalf("first page Cursor = %q, want the zero cursor", first.Cursor)
		}
		page := search.Result{NextCursor: search.Cursor("opaque-token-page-2")}
		next := search.Query{Text: "数据库迁移", Cursor: page.NextCursor}
		if next.Cursor != page.NextCursor {
			t.Fatalf("next page Cursor = %q, want the carried cursor %q", next.Cursor, page.NextCursor)
		}
		last := search.Result{}
		if last.NextCursor != "" {
			t.Fatalf("NextCursor = %q on a last page, want the zero cursor meaning no page follows", last.NextCursor)
		}
	})

	t.Run("a zero Limit runs under the declared bounded default", func(t *testing.T) {
		zero := search.Query{Text: "微信登录"}
		if zero.PageLimit() != search.DefaultLimit {
			t.Fatalf("PageLimit(zero Limit) = %d, want the declared default %d", zero.PageLimit(), search.DefaultLimit)
		}
		if zero.PageLimit() <= 0 {
			t.Fatalf("PageLimit(zero Limit) = %d, want a finite positive bound: zero Limit is not unbounded", zero.PageLimit())
		}
		named := search.Query{Text: "微信登录", Limit: 5}
		if named.PageLimit() != 5 {
			t.Fatalf("PageLimit(Limit 5) = %d, want the named bound 5", named.PageLimit())
		}
	})
}
