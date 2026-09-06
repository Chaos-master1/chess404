# Mimosa deep-scan triage — 2026-09-06

Deliberate pre-launch triage of the sealed 2026-09-06 deep scan, superseding
the quick pass noted in `docs/launch-audit/production-checklist.md` for the
2026-09-02 scan.

- Scan ID: `scan-2026-09-06T00-49-28.313Z-5b852ec787cb`
- Seal: `sha256:81100e660d773b8c1d00208a9364b89f941b36d82de1670796e66bbbe86aadd4`
- Artifacts: `~/.mimosa/security-scans/project-1d379e255e279db4efa9c218/scan-2026-09-06T00-49-28.313Z-5b852ec787cb/`
- Depth: deep. Run status reported as **inconclusive** (call graph partial:
  dynamic dispatch / analysis-size limits), so reachability conclusions below
  are manual reviews, not scanner output.
- Totals: 152 findings (49 high / 103 medium / 0 low). Dependency summary:
  800 packages scanned, advisories matched on 4 packages (29 advisory
  matches); the per-package breakdown is not reproduced in the sealed
  report.md — consult the scan artifacts for details.

## Dispositions

### 1. Untracked local test artifacts (~47 findings, incl. 33 high)

All findings under `playwright-report/` (Playwright's own bundled trace
viewer: `defaultSettingsView-*.js`, `sw.bundle.js`, `uiMode.*.js`,
`codeMirrorModule-*.js`). The directory is gitignored (`.gitignore:40`,
untracked since `49357bf`) — the scanner walked the working tree, not the
repo. Minified third-party code contains no first-party taint.

**Action: none.** One matchmaking finding (`cmd/matchmaking-service/main.go`
→ `codeMirrorModule-*.js` child_process) links first-party code to this
untracked asset across files; the chain is a scanner artifact.

### 2. Third-party minified browser assets (~30 findings)

`apps/web/public/stockfish*.js` (6 files) — vendored Stockfish WASM/JS
shipped as public static assets. Findings are pattern matches inside minified
internals ("ssrf entry" on a minified symbol), with no server-side data flow
into them.

**Action: none.** They are pinned vendor files; treat upstream updates as the
review point.

### 3. Local Python dev tools (~5 findings)

`internal/engine/nnue/pytrainer/network.py` (path traversal: `open(path,"wb")`
with a CLI-supplied path), `scripts/train_nnue.py`,
`scripts/generate_opening_book.py`. These are developer-run CLI tools writing
developer-chosen files. No Go service imports Python; the production
Dockerfiles do not run Python; there is no untrusted input.

**Action: none.**

### 4. First-party findings reviewed individually

| Finding | Verdict | Basis |
|---|---|---|
| `anticheat/stockfish.go:83` command injection (high) | False positive | `exec.Command(binary)` never spawns a shell; `binary` comes from operator config (`StockfishConfig.Binary`/`LookPath`), not from any request payload |
| `app/api/{gateway,matchmaking,platform}/_lib/proxy.ts` SSRF (medium ×4) | False positive, with a real follow-up | Internal service base URLs come from operator env vars — config, not user input. The related operational defect (bare-`:` URLs silently falling back to `:8080`) is already tracked in CLAUDE.md; fix the env vars |
| `apps/web/src/AccountPage.tsx` → `platform-service.ts` SSRF (1 high + 19 medium) | False positive | `fetchAccount` and friends are client-side same-origin `fetch` calls from the browser bundle; SSRF is a server-side class, and the browser cannot reach internal services except through the origin proxy |
| `routes_accounts.go:224`, `routes_guests.go:154` → `guests_sqlite.go:299/330` SQL injection (medium ×2) | False positive | Every statement in `guests_sqlite.go` uses bound `?` parameters or constant SQL; `limit` values pass through `ParseListLimit` (int-clamped); no `fmt.Sprintf` SQL construction exists in the file |

**No code changes were required by this scan.** The pre-existing structural
notes that remain true and worth keeping on the radar (from CLAUDE.md, not
this scan): the single shared internal service token, and the unauthenticated
public roster endpoints.

## What would change these dispositions

- Any route that interpolates request data into an internal URL path or the
  `stockfish` binary path.
- Any store method that builds SQL via string concatenation.
- Vendored `stockfish*.js` upgrades (re-triage the replaced file).
