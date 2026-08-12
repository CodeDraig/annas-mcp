# Anna's Archive CLI

A command-line tool for searching and downloading books and articles from [Anna's Archive](https://annas-archive.gl), with an optional repository-scoped Codex skill and automatic mirror selection through [SLUM](https://open-slum.org/).

> [!NOTE]
> This tool can retrieve permissively licensed and public-domain documents. It does not authorize acquisition of copyrighted material. Respect authors' rights and applicable law.

> [!WARNING]
> See [Mirror Selection](#mirror-selection) if Anna's Archive links are unavailable.

## Requirements

Search works without configuration. Downloads require:

- [Anna's Archive JSON API access](https://annas-archive.gl/donate)
- An [Anna's Archive API key](https://annas-archive.gl/faq#api)
- An absolute download directory

## Install

Download `annas-cli` from the [GitHub Releases page](https://github.com/iosifache/annas-mcp/releases), or build it from source:

```bash
go build -o annas-cli ./cmd/annas-cli
```

## Use with Codex

The repository contains the `$annas-archive` skill under `.agents/skills`. Launch Codex from this repository and invoke the skill explicitly, or ask to search or download a book or article from Anna's Archive. The skill uses `annas-cli` from `PATH` when installed and falls back to `go run ./cmd/annas-cli` in the source checkout.

The skill is repository-scoped and is not included in release archives.

## Configuration

Set variables in the environment or in an `.env` file beside the working command:

- `ANNAS_SECRET_KEY`: Anna's Archive API key used for downloads.
- `ANNAS_DOWNLOAD_PATH`: Absolute directory for downloaded documents.
- `ANNAS_BASE_URL`: Mirror hostname; defaults to `annas-archive.gl`.
- `ANNAS_AUTO_BASE_URL`: Set to `true` to select a reachable mirror through SLUM.

### Mirror Selection

Automatic discovery is opt-in. When `ANNAS_AUTO_BASE_URL=true`, the CLI ranks Anna's Archive mirrors by recent health and latency, probes them locally, and selects the best reachable mirror. If discovery fails, it falls back to `ANNAS_BASE_URL`, then the built-in default.

### Timeouts

HTTP requests default to one hour. Override the timeout with a persistent flag:

```bash
annas-cli --timeout 30m book-search "machine learning"
```

## Output

Operational commands emit one JSON object to stdout. Search responses contain arrays even when no results are found:

```json
{"operation":"book-search","data":{"query":"machine learning","books":[]}}
```

Downloads report the exact created path. Article downloads also report whether the fast-download API or SciDB fallback succeeded.

Use `--human` for human-readable output:

```bash
annas-cli --human article-search "10.1038/nature12345"
```

Errors return a nonzero status and write diagnostics to stderr.

## Commands

| Operation | Command | Example |
| --- | --- | --- |
| Search for books | `book-search` | `annas-cli book-search "machine learning python"` |
| Download a book by MD5 | `book-download` | `annas-cli book-download abc123def456 "my-book.pdf"` |
| Search for articles | `article-search` | `annas-cli article-search "neural networks"` |
| Look up an article by DOI | `article-search` | `annas-cli article-search "10.1038/nature12345"` |
| Download an article by DOI | `article-download` | `annas-cli article-download "10.1038/nature12345"` |
