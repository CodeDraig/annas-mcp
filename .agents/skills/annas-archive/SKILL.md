---
name: annas-archive
description: Search Anna's Archive for books or articles and download explicitly requested results through the local annas-cli executable. Use when a user asks to find, look up, or download a book, paper, article, DOI, author, title, or topic from Anna's Archive.
---

# Anna's Archive

Use `annas-cli` as the only execution interface. Consume its default JSON output; do not scrape human-readable output.

## Resolve the CLI

1. Run `annas-cli` from `PATH` when available.
2. Otherwise, resolve the repository root and run `go run ./cmd/annas-cli` from that root.
3. Stop and report the missing executable if neither path exists. Do not install global software implicitly.

Append `--timeout <duration>` only when the user requests a different timeout. Use `--human` only when the user explicitly wants terminal-oriented presentation.

## Search

- Search for books with `annas-cli book-search "<query>"`.
- Search for articles or look up a DOI with `annas-cli article-search "<query-or-doi>"`.
- Read books from `data.books` and articles from `data.articles` in the response envelope.
- Treat an empty array as a successful search with no results. Treat a nonzero exit status as a failure and report the stderr diagnostic instead of claiming no results.
- Present relevant metadata and retain the returned hash or DOI when the user may request a follow-up download.

## Download

Download only when the user explicitly requests a document. Do not infer download permission from a search request.

1. Verify that `ANNAS_SECRET_KEY` and `ANNAS_DOWNLOAD_PATH` are non-empty without printing either value.
2. Download a selected book with `annas-cli book-download "<hash>" "<filename.ext>"`.
3. Download a selected article with `annas-cli article-download "<doi>"`.
4. Report the exact path from `data.path`. For article downloads, also report `data.method` as `fast-download` or `scidb`.

Never echo, log, or include `ANNAS_SECRET_KEY` in the response. Preserve the user's requested title and format when constructing a book filename, and do not invent a hash or DOI.
