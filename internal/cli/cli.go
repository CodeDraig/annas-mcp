package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/iosifache/annas-mcp/internal/anna"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"github.com/iosifache/annas-mcp/internal/version"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func Start() {
	l := logger.GetLogger()
	defer l.Sync()

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		l.Warn("Error loading .env file", zap.Error(err))
	}

	if err := fang.Execute(
		context.Background(),
		NewCommand(),
		fang.WithVersion(version.GetVersion()),
	); err != nil {
		os.Exit(1)
	}
}

func NewCommand() *cobra.Command {
	l := logger.GetLogger()
	timeout := anna.DefaultHTTPTimeout
	human := false

	rootCmd := &cobra.Command{
		Use:   "annas-cli",
		Short: "Anna's Archive CLI",
		Long:  "Search and download books and articles from Anna's Archive. Automatic mirror discovery uses SLUM (https://open-slum.org/).",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Version: version.GetVersion(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return fmt.Errorf("timeout must be greater than zero")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", anna.DefaultHTTPTimeout, "HTTP request timeout, e.g. 30s, 10m, 1h")
	rootCmd.PersistentFlags().BoolVar(&human, "human", false, "render human-readable output instead of JSON")

	rootCmd.AddCommand(
		newBookSearchCommand(&timeout, &human, l),
		newBookDownloadCommand(&timeout, &human, l),
		newArticleSearchCommand(&timeout, &human, l),
		newArticleDownloadCommand(&timeout, &human, l),
	)

	return rootCmd
}

func newBookSearchCommand(timeout *time.Duration, human *bool, l *zap.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "book-search [query]",
		Short: "Search for books by title, author, or topic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			l.Info("Book search command called", zap.String("query", query))

			books, err := anna.FindBook(query, *timeout)
			if err != nil {
				l.Error("Book search command failed", zap.String("query", query), zap.Error(err))
				return fmt.Errorf("failed to search books: %w", err)
			}

			l.Info("Book search command completed successfully", zap.String("query", query), zap.Int("resultsCount", len(books)))
			return renderBookSearch(cmd.OutOrStdout(), *human, query, books)
		},
	}
}

func newBookDownloadCommand(timeout *time.Duration, human *bool, l *zap.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "book-download [hash] [filename]",
		Short: "Download a book by its MD5 hash",
		Long:  "Download a book by its MD5 hash to the specified filename. Requires ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bookHash := args[0]
			filename := args[1]

			ext := filepath.Ext(filename)
			if ext == "" {
				return fmt.Errorf("filename must include an extension (e.g., .pdf, .epub)")
			}
			format := strings.TrimPrefix(ext, ".")
			title := strings.TrimSuffix(filepath.Base(filename), ext)

			l.Info("Download command called",
				zap.String("bookHash", bookHash),
				zap.String("filename", filename),
				zap.String("title", title),
				zap.String("format", format),
			)

			cfg, err := env.GetEnv()
			if err != nil {
				l.Error("Failed to get environment variables", zap.Error(err))
				return fmt.Errorf("failed to get environment: %w", err)
			}

			book := &anna.Book{Hash: bookHash, Title: title, Format: format}
			path, err := book.Download(cfg.SecretKey, cfg.DownloadPath, *timeout)
			if err != nil {
				l.Error("Download command failed", zap.String("bookHash", bookHash), zap.String("downloadPath", cfg.DownloadPath), zap.Error(err))
				return fmt.Errorf("failed to download book: %w", err)
			}

			l.Info("Download command completed successfully", zap.String("bookHash", bookHash), zap.String("path", path))
			return renderDownload(cmd.OutOrStdout(), *human, "book-download", "Book", path, "")
		},
	}
}

func newArticleSearchCommand(timeout *time.Duration, human *bool, l *zap.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "article-search [query]",
		Short: "Search for articles by DOI or keywords",
		Long:  "Search for academic articles. Provide a DOI (starting with '10.') or keywords.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			l.Info("Article search command called", zap.String("query", query))

			if strings.HasPrefix(strings.TrimSpace(query), "10.") {
				paper, err := anna.LookupDOI(query, *timeout)
				if err != nil {
					l.Error("DOI lookup failed", zap.String("doi", query), zap.Error(err))
					return fmt.Errorf("DOI lookup failed: %w", err)
				}

				l.Info("DOI lookup completed", zap.String("doi", query))
				return renderArticleSearch(cmd.OutOrStdout(), *human, true, query, []*anna.Paper{paper})
			}

			papers, err := anna.FindArticle(query, *timeout)
			if err != nil {
				l.Error("Article search failed", zap.String("query", query), zap.Error(err))
				return fmt.Errorf("article search failed: %w", err)
			}

			l.Info("Article search completed successfully", zap.String("query", query), zap.Int("resultsCount", len(papers)))
			return renderArticleSearch(cmd.OutOrStdout(), *human, false, query, papers)
		},
	}
}

func newArticleDownloadCommand(timeout *time.Duration, human *bool, l *zap.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "article-download [doi]",
		Short: "Download an article by its DOI",
		Long:  "Download an academic article or paper by providing its DOI.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doi := args[0]
			l.Info("Article download command called", zap.String("doi", doi))

			cfg, err := env.GetEnv()
			if err != nil {
				l.Error("Failed to get environment variables", zap.Error(err))
				return fmt.Errorf("failed to get environment: %w", err)
			}

			paper, err := anna.LookupDOI(doi, *timeout)
			if err != nil {
				l.Error("DOI lookup failed for download", zap.String("doi", doi), zap.Error(err))
				return fmt.Errorf("DOI lookup failed: %w", err)
			}

			if paper.Hash != "" && cfg.SecretKey != "" {
				book := &anna.Book{Hash: paper.Hash, Title: paper.Title, Format: "pdf"}
				path, downloadErr := book.Download(cfg.SecretKey, cfg.DownloadPath, *timeout)
				if downloadErr == nil {
					l.Info("Article downloaded via fast download", zap.String("doi", doi), zap.String("path", path))
					return renderDownload(cmd.OutOrStdout(), *human, "article-download", "Article", path, "fast-download")
				}
				l.Warn("Fast download failed, trying SciDB download", zap.String("doi", doi), zap.Error(downloadErr))
			}

			path, err := paper.Download(cfg.DownloadPath, *timeout)
			if err != nil {
				l.Error("SciDB download failed", zap.String("doi", doi), zap.Error(err))
				return fmt.Errorf("download failed: %w", err)
			}

			l.Info("Article downloaded via SciDB", zap.String("doi", doi), zap.String("path", path))
			return renderDownload(cmd.OutOrStdout(), *human, "article-download", "Article", path, "scidb")
		},
	}
}
