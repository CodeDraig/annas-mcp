package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iosifache/annas-mcp/internal/anna"
)

func TestSearchCommandsPassPagination(t *testing.T) {
	for _, name := range []string{"book-search", "article-search"} {
		for _, mode := range []string{"default", "count", "all", "all-later"} {
			var received anna.SearchOptions
			ops := operations{
				findBooks: func(_ context.Context, _ string, _ time.Duration, options anna.SearchOptions) ([]*anna.Book, error) {
					received = options
					return nil, nil
				},
				findArticles: func(_ context.Context, _ string, _ time.Duration, options anna.SearchOptions) ([]*anna.Paper, error) {
					received = options
					return nil, nil
				},
			}
			command := newCommand(ops)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			args := []string{name, "graphs"}
			want := anna.SearchOptions{Page: 1, Pages: 1}
			if mode == "count" {
				args = append(args, "--page", "3", "--pages", "2")
				want = anna.SearchOptions{Page: 3, Pages: 2}
			} else if mode == "all" || mode == "all-later" {
				args = append(args, "--all-pages")
				want.AllPages = true
				if mode == "all-later" {
					args = append(args, "--page", "4")
					want.Page = 4
				}
			}
			command.SetArgs(args)
			if err := command.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if received != want {
				t.Fatalf("%s mode=%s passed %+v, want %+v", name, mode, received, want)
			}
		}
	}
}

func TestSearchCommandsRejectInvalidPagination(t *testing.T) {
	for _, name := range []string{"book-search", "article-search"} {
		for _, flags := range [][]string{{"--page", "0"}, {"--page", "101"}, {"--pages", "0"}, {"--pages", "-1"}, {"--all-pages", "--pages", "2"}, {"--pages", "1", "--all-pages"}} {
			command := newCommand(operations{})
			var stdout bytes.Buffer
			command.SetOut(&stdout)
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(append([]string{name, "graphs"}, flags...))
			if err := command.ExecuteContext(context.Background()); err == nil {
				t.Fatalf("%s accepted invalid flags %v", name, flags)
			}
			if stdout.Len() != 0 {
				t.Fatalf("invalid pagination wrote stdout: %s", stdout.String())
			}
		}
	}
}

func TestDOILookupRejectsPagination(t *testing.T) {
	for _, flags := range [][]string{{"--pages", "2"}, {"--all-pages"}} {
		command := newCommand(operations{lookupDOI: func(context.Context, string, time.Duration) (*anna.Paper, error) {
			t.Fatal("pagination must fail before DOI lookup")
			return nil, nil
		}})
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs(append([]string{"article-search", "10.1234/example"}, flags...))
		if err := command.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "DOI") {
			t.Fatalf("DOI pagination returned %v", err)
		}
	}
}
