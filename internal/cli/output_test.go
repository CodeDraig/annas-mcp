package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iosifache/annas-mcp/internal/anna"
)

func TestRenderBookSearchJSON(t *testing.T) {
	var output bytes.Buffer
	books := []*anna.Book{{Title: "A Book", Hash: "abc123", Format: "PDF"}}

	if err := renderBookSearch(&output, false, "books", books); err != nil {
		t.Fatalf("renderBookSearch returned error: %v", err)
	}

	var got response[bookSearchData]
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.Operation != "book-search" || got.Data.Query != "books" {
		t.Fatalf("unexpected envelope: %+v", got)
	}
	if !reflect.DeepEqual(got.Data.Books, books) {
		t.Fatalf("unexpected books: %#v", got.Data.Books)
	}
}

func TestRenderSearchesUseEmptyArrays(t *testing.T) {
	var bookOutput bytes.Buffer
	if err := renderBookSearch(&bookOutput, false, "missing", nil); err != nil {
		t.Fatalf("renderBookSearch returned error: %v", err)
	}
	if !strings.Contains(bookOutput.String(), `"books":[]`) {
		t.Fatalf("expected empty books array, got %s", bookOutput.String())
	}

	var articleOutput bytes.Buffer
	if err := renderArticleSearch(&articleOutput, false, false, "missing", nil); err != nil {
		t.Fatalf("renderArticleSearch returned error: %v", err)
	}
	if !strings.Contains(articleOutput.String(), `"articles":[]`) {
		t.Fatalf("expected empty articles array, got %s", articleOutput.String())
	}
}

func TestRenderArticleSearchJSONUsesArray(t *testing.T) {
	var output bytes.Buffer
	articles := []*anna.Paper{{DOI: "10.1234/example", Title: "A Paper"}}

	if err := renderArticleSearch(&output, false, true, "10.1234/example", articles); err != nil {
		t.Fatalf("renderArticleSearch returned error: %v", err)
	}

	var got response[articleSearchData]
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.Operation != "article-search" || len(got.Data.Articles) != 1 {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

func TestRenderDownloadJSON(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		method    string
	}{
		{name: "book", operation: "book-download"},
		{name: "article", operation: "article-download", method: "scidb"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := renderDownload(&output, false, test.operation, "Item", "/tmp/item.pdf", test.method); err != nil {
				t.Fatalf("renderDownload returned error: %v", err)
			}

			var got response[downloadData]
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if got.Operation != test.operation || got.Data.Path != "/tmp/item.pdf" || got.Data.Method != test.method {
				t.Fatalf("unexpected envelope: %+v", got)
			}
		})
	}
}

func TestRenderHumanOutput(t *testing.T) {
	var searchOutput bytes.Buffer
	if err := renderBookSearch(&searchOutput, true, "missing", nil); err != nil {
		t.Fatalf("renderBookSearch returned error: %v", err)
	}
	if got := searchOutput.String(); got != "No books found.\n" {
		t.Fatalf("unexpected human search output: %q", got)
	}

	var downloadOutput bytes.Buffer
	if err := renderDownload(&downloadOutput, true, "book-download", "Book", "/tmp/book.pdf", ""); err != nil {
		t.Fatalf("renderDownload returned error: %v", err)
	}
	if got := downloadOutput.String(); got != "Book downloaded successfully to: /tmp/book.pdf\n" {
		t.Fatalf("unexpected human download output: %q", got)
	}
}

func TestRenderHumanDOILookupPreservesSinglePaperFormat(t *testing.T) {
	var output bytes.Buffer
	article := &anna.Paper{DOI: "10.1234/example", Title: "A Paper"}

	if err := renderArticleSearch(&output, true, true, article.DOI, []*anna.Paper{article}); err != nil {
		t.Fatalf("renderArticleSearch returned error: %v", err)
	}
	if strings.HasPrefix(output.String(), "Article 1:") {
		t.Fatalf("DOI lookup unexpectedly gained a list prefix: %q", output.String())
	}
	if !strings.HasPrefix(output.String(), "DOI: 10.1234/example\n") {
		t.Fatalf("unexpected DOI lookup output: %q", output.String())
	}
}

func TestCommandSurfaceContainsOnlyCLIOperations(t *testing.T) {
	root := NewCommand()
	got := make([]string, 0, len(root.Commands()))
	for _, command := range root.Commands() {
		got = append(got, command.Name())
	}

	want := []string{"article-download", "article-search", "book-download", "book-search"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected commands: got %v, want %v", got, want)
	}
}

func TestBookSearchFailureReturnsErrorWithoutJSON(t *testing.T) {
	command := newCommand(operations{
		findBooks: func(context.Context, string, time.Duration) ([]*anna.Book, error) {
			return nil, errors.New("browser verification timed out")
		},
	})
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"book-search", "mothman"})

	err := command.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "browser verification timed out") {
		t.Fatalf("ExecuteContext returned %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed search wrote JSON: %q", stdout.String())
	}
}

func TestBookSearchPassesCommandContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newCommand(operations{
		findBooks: func(received context.Context, _ string, _ time.Duration) ([]*anna.Book, error) {
			if !errors.Is(received.Err(), context.Canceled) {
				t.Fatalf("operation context error = %v, want canceled", received.Err())
			}
			return nil, received.Err()
		},
	})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"book-search", "mothman"})
	if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteContext returned %v, want context canceled", err)
	}
}
