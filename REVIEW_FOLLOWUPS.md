# Repository Review Follow-ups

These open items come from the static repository review of commit
`8be07bcd5e310cd7065a1f6396ff1ab73bf16915`. They are intentionally documented
for later work and are not implemented as part of the ACT-NOW corrections.

## [CONTAIN] Bound automatic-mirror metadata responses

- **Status:** Open; contain by leaving `ANNAS_AUTO_BASE_URL` unset or `false`.
- **Location:** `internal/mirror/resolver.go`, `Resolver.fetch`
- **Evidence:** Status-page HTML and heartbeat JSON are consumed with
  `io.ReadAll(resp.Body)` without a byte limit.
- **Impact signature:** `TRIGGER: ADVERSARIAL | SCOPE: INSTANCE | STATE: EPHEMERAL | RECOVERY: RESTART | CONTAINMENT: DISABLE | FIX: LOCAL`
- **Failure envelope:** A compromised or malfunctioning status service can send
  enough data to exhaust memory in a CLI invocation. Explicit mirror selection
  is unaffected.
- **Proposed repair:** Apply a documented maximum through `io.LimitReader`,
  detect a response beyond that maximum, and fall back with a distinct error.
- **Validation:** Test responses at the limit and one byte above it with the
  existing fake HTTP transport.

## [DEFER] Return asynchronous search transport failures

- **Status:** Open.
- **Location:** `internal/anna/anna.go`, `FindBook` and `FindArticle`
- **Evidence:** Both collectors use `colly.Async(true)`; their `OnError`
  callbacks only log, and `Wait` returns no error. A failed request can therefore
  become a successful empty result.
- **Impact signature:** `TRIGGER: ROUTINE | SCOPE: REQUEST | STATE: EPHEMERAL | RECOVERY: RETRY | CONTAINMENT: NONE | FIX: CROSSCUTTING`
- **Failure envelope:** Network errors and HTTP failures are indistinguishable
  from a real search with no matches.
- **Proposed repair:** Remove asynchronous mode for these single-request
  collectors, or capture and return the first callback error safely after
  `Wait`.
- **Validation:** Inject connection failures, timeouts, and HTTP 500 responses;
  require a nonzero CLI result and no successful empty-results envelope.

## [DEFER] Make tag recreation recoverable

- **Status:** Open; avoid `scripts/manage-tag.sh recreate` until corrected.
- **Location:** `scripts/manage-tag.sh`, `recreate` action
- **Evidence:** The action deletes the local and remote tag before creating and
  pushing its replacement, suppressing deletion failures and preserving no prior
  remote object ID.
- **Impact signature:** `TRIGGER: ADMIN | SCOPE: REPOSITORY | STATE: MUTATED | RECOVERY: EDIT | CONTAINMENT: AVOID | FIX: LOCAL`
- **Failure envelope:** Authentication, policy, or network failure after remote
  deletion leaves the release tag absent.
- **Proposed repair:** Resolve and retain the old remote object ID, validate the
  replacement locally, and use one guarded remote ref update with an exact lease
  instead of delete-then-push.
- **Validation:** Use a disposable bare remote with a deliberately rejected
  replacement and verify that the original remote tag remains unchanged.
