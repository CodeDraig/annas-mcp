# Anna's Archive CLI

A command-line tool for searching and downloading books and articles from [Anna's Archive](https://annas-archive.gl), with an optional repository-scoped Codex skill and automatic mirror selection through [SLUM](https://open-slum.org/).

> [!NOTE]
> This tool can retrieve permissively licensed and public-domain documents. It does not authorize acquisition of copyrighted material. Respect authors' rights and applicable law.

> [!WARNING]
> See [Mirror Selection](#mirror-selection) if Anna's Archive links are unavailable.

## Requirements

Search works without configuration. When a mirror presents DDoS-Guard browser
verification, search also requires an installed Chrome or Chromium browser and
a graphical desktop where the verification window can open.

Downloads require:

- [Anna's Archive JSON API access](https://annas-archive.gl/donate)
- An [Anna's Archive API key](https://annas-archive.gl/faq#api)
- A complete per-user configuration file containing the API key, an absolute
  download directory, a trusted mirror hostname, and the automatic-discovery
  setting

## Install

Download `annas-cli` from the [GitHub Releases page](https://github.com/iosifache/annas-mcp/releases), or build it from source:

```bash
go build -o annas-cli ./cmd/annas-cli
```

## Use with Codex

The repository contains the `$annas-archive` skill under `.agents/skills`. Launch Codex from this repository and invoke the skill explicitly, or ask to search or download a book or article from Anna's Archive. The skill uses `annas-cli` from `PATH` when installed and falls back to `go run ./cmd/annas-cli` in the source checkout.

The skill is repository-scoped and is not included in release archives.

## Configuration

The CLI reads configuration only from `annas-cli/.env` inside the user
configuration directory returned by the operating system. On Linux this is:

```text
$XDG_CONFIG_HOME/annas-cli/.env
```

or `$HOME/.config/annas-cli/.env` when `XDG_CONFIG_HOME` is unset. The CLI does
not read `ANNAS_*` values from the inherited process environment or from an
`.env` file in the invocation directory.

Downloads require all four settings:

```dotenv
ANNAS_SECRET_KEY=replace-with-your-api-key
ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads
ANNAS_BASE_URL=annas-archive.gl
ANNAS_AUTO_BASE_URL=false
```

- `ANNAS_SECRET_KEY`: Anna's Archive API key used only for downloads.
- `ANNAS_DOWNLOAD_PATH`: Existing absolute directory for downloaded documents. Create it before downloading.
- `ANNAS_BASE_URL`: Trusted mirror hostname used for API-key-authenticated downloads and as the public-request fallback.
- `ANNAS_AUTO_BASE_URL`: `true` to select a reachable mirror through SLUM; `false` to use `ANNAS_BASE_URL`.

Public search and DOI lookup retain only `ANNAS_BASE_URL` and
`ANNAS_AUTO_BASE_URL` from this file; the public configuration path never parses
`ANNAS_SECRET_KEY` or `ANNAS_DOWNLOAD_PATH`. If either public setting is absent,
its built-in default is used. Invalid public settings fail instead of silently
falling back. Downloads still require a complete valid file and report the
expected path when it is missing.

### Mirror Selection

Automatic discovery is opt-in. When `ANNAS_AUTO_BASE_URL=true`, the CLI reads
the current Anna's Archive group from SLUM's FlatMonitor dashboard, ranks
canonical mirrors by status and recent latency, probes ordinary mirrors, and
selects the best candidate for public search, DOI, and SciDB requests. If
discovery fails, it falls back to `ANNAS_BASE_URL`, then the built-in default.
API-key-authenticated fast-download requests never use an automatically
discovered mirror; they require and use the configured `ANNAS_BASE_URL`.
Authenticated API redirects must retain HTTPS and the configured hostname and
port, and omit the `Referer` header. A service redirect to another origin fails;
verify the new host before changing `ANNAS_BASE_URL`. File URLs returned by the
API use a separate request and can follow redirects to download hosts.

### Browser Verification

The CLI tries ordinary HTTP first. If the returned page is specifically a
DDoS-Guard challenge, it opens a visible Chrome or Chromium window. Complete
the check in that window and leave the command running; the CLI captures the
verified page and continues normally. Generic HTTP 403 responses do not launch
the browser.

Browser state is isolated from the regular browser profile and retained at:

```text
$XDG_CONFIG_HOME/annas-cli/browser-profile
```

or `$HOME/.config/annas-cli/browser-profile` when `XDG_CONFIG_HOME` is unset.
The directory is private to the user. The CLI never loads `ANNAS_SECRET_KEY`
into this browser profile or sends it during search. If Chrome, a graphical
display, or manual verification is unavailable, the command exits nonzero with
an actionable diagnostic instead of returning an empty result.

### Timeouts

HTTP requests and interactive browser verification default to one hour. The
complete automatic-discovery phase also uses this timeout, with individual mirror
probes capped at ten seconds. The timeout applies separately to discovery and
each subsequent request, rather than to the entire command. Ctrl-C cancels active
requests, discovery, and browser verification; canceled article downloads do not
start a fallback request. Override the timeout with a persistent flag:

```bash
annas-cli --timeout 30m book-search "machine learning"
```

## Output

Operational commands emit one JSON object to stdout. Search responses contain arrays even when no results are found:

```json
{"operation":"book-search","data":{"query":"machine learning","books":[]}}
```

Downloads report the exact created path and fail without changing the destination if that path already exists. Document downloads reject HTML responses before creating a file; explicitly requested TXT/HTML book formats remain allowed. Article downloads also report whether the fast-download API or SciDB fallback succeeded.

Use `--human` for human-readable output:

```bash
annas-cli --human article-search "10.1038/nature12345"
```

Errors return a nonzero status and write diagnostics to stderr. Browser
verification progress also uses stderr, leaving stdout reserved for the final
result object.

## Commands

Pass DOI identifiers as quoted, unescaped text. The CLI encodes reserved URL
characters such as `#`, `?`, `&`, `+`, and `%` for lookup and fallback downloads.
Do not manually percent-encode the DOI; literal percent sequences remain part of
the identifier. Returned DOI metadata preserves the supplied identifier.

| Operation | Command | Example |
| --- | --- | --- |
| Search for books | `book-search` | `annas-cli book-search "machine learning python"` |
| Download a book by MD5 | `book-download` | `annas-cli book-download abc123def456 "my-book.pdf"` |
| Search for articles | `article-search` | `annas-cli article-search "neural networks"` |
| Look up an article by DOI | `article-search` | `annas-cli article-search "10.1038/nature12345"` |
| Download an article by DOI | `article-download` | `annas-cli article-download "10.1038/nature12345"` |

## Tag maintenance

Run `bash scripts/manage-tag.sh recreate` from the repository root to replace the
version tag with a lightweight tag at the current commit. The script records the
old local and remote object IDs, then publishes one replacement guarded by the
exact remote ID. Rejected pushes preserve the local tag and do not delete the
remote tag. A concurrent remote edit causes the lease to fail. After publication,
the local tag is updated only if it still matches its original value.

If publication succeeds but the local tag changes concurrently, the script reports
the published commit and preserves the local edit for manual reconciliation.
After an ambiguous network failure, inspect the remote tag before retrying.
Run `bash scripts/manage-tag_test.sh` for disposable local-remote regression tests.
