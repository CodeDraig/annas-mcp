package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/iosifache/annas-mcp/internal/anna"
)

type response[T any] struct {
	Operation string `json:"operation"`
	Data      T      `json:"data"`
}

type bookSearchData struct {
	Query string       `json:"query"`
	Books []*anna.Book `json:"books"`
}

type articleSearchData struct {
	Query    string        `json:"query"`
	Articles []*anna.Paper `json:"articles"`
}

type downloadData struct {
	Path   string `json:"path"`
	Method string `json:"method,omitempty"`
}

func renderBookSearch(w io.Writer, human bool, query string, books []*anna.Book) error {
	if books == nil {
		books = []*anna.Book{}
	}

	if human {
		if len(books) == 0 {
			_, err := fmt.Fprintln(w, "No books found.")
			return err
		}
		for i, book := range books {
			if _, err := fmt.Fprintf(w, "Book %d:\n%s\n", i+1, book.String()); err != nil {
				return err
			}
			if i < len(books)-1 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
		}
		return nil
	}

	return writeJSON(w, response[bookSearchData]{
		Operation: "book-search",
		Data:      bookSearchData{Query: query, Books: books},
	})
}

func renderArticleSearch(w io.Writer, human, doiLookup bool, query string, articles []*anna.Paper) error {
	if articles == nil {
		articles = []*anna.Paper{}
	}

	if human {
		if len(articles) == 0 {
			_, err := fmt.Fprintln(w, "No articles found.")
			return err
		}
		if doiLookup {
			_, err := fmt.Fprintln(w, articles[0].String())
			return err
		}
		for i, article := range articles {
			if _, err := fmt.Fprintf(w, "Article %d:\n%s\n", i+1, article.String()); err != nil {
				return err
			}
			if i < len(articles)-1 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
		}
		return nil
	}

	return writeJSON(w, response[articleSearchData]{
		Operation: "article-search",
		Data:      articleSearchData{Query: query, Articles: articles},
	})
}

func renderDownload(w io.Writer, human bool, operation, noun, path, method string) error {
	if human {
		_, err := fmt.Fprintf(w, "%s downloaded successfully to: %s\n", noun, path)
		return err
	}

	return writeJSON(w, response[downloadData]{
		Operation: operation,
		Data:      downloadData{Path: path, Method: method},
	})
}

func writeJSON(w io.Writer, value any) error {
	return json.NewEncoder(w).Encode(value)
}
