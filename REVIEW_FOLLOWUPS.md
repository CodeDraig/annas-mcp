# Repository Review Follow-ups

This ledger records the earlier review of commit
`8be07bcd5e310cd7065a1f6396ff1ab73bf16915` and corrections following the
2026-09-04 working-tree review. The response-size item remains open.

## [CONTAIN] Bound automatic-mirror metadata responses

- **Status:** Open; contain by leaving `ANNAS_AUTO_BASE_URL` unset or `false`.
- **Location:** `internal/mirror/resolver.go`, `Resolver.fetch`
- **Evidence:** Status-page HTML is consumed with `io.ReadAll(resp.Body)`
  without a byte limit.
- **Impact signature:** `TRIGGER: ADVERSARIAL | SCOPE: INSTANCE | STATE: EPHEMERAL | RECOVERY: RESTART | CONTAINMENT: DISABLE | FIX: LOCAL`
- **Failure envelope:** A compromised or malfunctioning status service can send
  enough data to exhaust memory in a CLI invocation. Explicit mirror selection
  is unaffected.
- **Proposed repair:** Apply a documented maximum through `io.LimitReader`,
  detect a response beyond that maximum, and fall back with a distinct error.
- **Validation:** Test responses at the limit and one byte above it with the
  existing fake HTTP transport.

## [RESOLVED] Return search transport failures

- **Status:** Resolved by the browser-assisted search correction.
- **Location:** `internal/webpage/loader.go` and `internal/anna/search.go`
- **Evidence:** Public pages now load synchronously through one HTTP/browser
  boundary that returns transport, HTTP, and verification failures. Search
  renders an empty result only after a page loads successfully.
- **Impact signature:** `TRIGGER: ROUTINE | SCOPE: REQUEST | STATE: EPHEMERAL | RECOVERY: RETRY | CONTAINMENT: NONE | FIX: CROSSCUTTING`
- **Failure envelope:** Network errors and HTTP failures are indistinguishable
  from a real search with no matches.
- **Repair:** Removed Colly page acquisition, propagated loader errors, and
  suppressed Cobra usage output on operational failures so stdout contains no
  result envelope.
- **Validation:** Fake-loader and command tests cover HTTP failure, browser
  timeout/closure, successful empty results, nonzero errors, and empty stdout.

## [RESOLVED] Preserve tags when replacement fails

- **Assessment:** Recreation is an explicit maintainer operation; frequency remains
  unknown. The requested correction preserves the existing lightweight-at-HEAD
  semantics. Local disposable remotes exercise rejection and concurrent edits;
  no production tag or remote was modified.
- **Repair:** `scripts/manage-tag.sh` snapshots both object IDs, prepares HEAD,
  performs one exact-lease remote update, then compare-and-swaps the local ref.
  Errors identify the remaining reconciliation work without deleting either ref.
- **Validation:** `bash scripts/manage-tag_test.sh` covers rejected publication,
  annotated prior tags, one-step replacement, concurrent remote/local edits, and
  initially absent tags.

## [RESOLVED] Propagate cancellation through downloads and discovery

- **Repair:** Download HTTP requests receive the command context. DOI enrichment
  propagates cancellation, and canceled article downloads do not start fallback.
  Discovery has a caller-bounded deadline, stops on cancellation, and does not
  cache canceled attempts.
- **Validation:** Local HTTP tests cover stalled API/download headers, partial
  body cleanup, and command-level fallback suppression. Resolver/configuration
  tests cover canceled status fetches and probes, deadlines, and retry after
  canceled discovery.

## [RESOLVED] Validate browser status and document payloads

- **Repair:** Browser acquisition matches final document status to its frame and
  navigation, excluding subresources. Downloads inspect MIME type and a bounded
  prefix before persistence; explicit text formats remain supported.
- **Validation:** Real headless Chrome tests navigate challenge pages through
  redirects to 200/403/503 documents, including a failing iframe. Download tests
  reject HTML, preserve PDF/EPUB bytes, and verify article fallback after a rejected
  fast-download response.

## [RESOLVED] Keep authenticated redirects within the configured origin

- **Assessment:** A synthetic transport reproduced Go forwarding the canary API
  key in `Referer`. Live redirect incidence remains unknown. The configured host
  is the credential trust boundary; API redirects retain that host and port over
  HTTPS. File delivery has a separate client so CDN redirects remain supported.
- **Repair:** The authenticated client rejects origin changes and userinfo,
  removes redirect referrers, and retains the ten-redirect limit. No credential
  or production endpoint was used in the assessment.
- **Validation:** Synthetic tests cover same-origin success, other hosts/ports,
  downgrade, explicit key forwarding, userinfo, loops, redacted command errors,
  and complete book delivery through an API redirect and a separate CDN redirect.

## [RESOLVED] Isolate configuration-dependent integration fixtures

- **Evidence:** Repeating the tests in one process reproduced requests to a
  closed fixture server and writes to a deleted download directory.
- **Repair:** Configuration-dependent Anna and CLI tests execute once per fresh
  subprocess, preserving production configuration-cache semantics.
- **Validation:** Repeat the complete race-enabled suite with `go test -race
  -count=2 ./...`; fixture children also use the race-instrumented test binary.

## [RESOLVED] Preserve DOI identifiers in lookup and fallback URLs

- **Evidence:** Round-trip tests reproduced fragment truncation, query-parameter
  injection, plus-to-space conversion, and invalid or misinterpreted percent
  sequences in the previous direct string interpolation.
- **Repair:** Lookup URLs use an explicit URL path; fallback URLs encode the DOI
  as a query value. DOI metadata retains the raw input. Callers should supply raw
  identifiers rather than applying the earlier manual `%23` workaround.
- **Validation:** DOI tests compare decoded paths and query values with the
  original identifier, covering reserved characters, literal `%23`, nested
  slashes, Unicode, and the legacy Crossref DOI ending in `#`.
