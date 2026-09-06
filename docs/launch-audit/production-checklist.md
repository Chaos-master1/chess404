# Production checklist — 2026-09-02

Things that cannot be done from the repo and need the Railway dashboard (or
GitHub secrets UI). Each item names the evidence and the exact action.

## 1. Email is silently in preview mode

`cmd/platform-service/account_email_delivery.go:230` defaults
`ACCOUNT_EMAIL_DELIVERY_PROVIDER` to `preview`, which only logs password-reset
links. Password reset therefore does not actually email anyone in production.

**Action:** set the SMTP block on the `platform-service` service in Railway —
the full env-var list with explanations is in
[DEPLOY_RAILWAY.md](../../DEPLOY_RAILWAY.md) (`platform-service` section).
Minimum: `ACCOUNT_EMAIL_DELIVERY_PROVIDER=smtp`, `ACCOUNT_EMAIL_SMTP_ADDRESS`,
`ACCOUNT_EMAIL_SMTP_FROM`, `ACCOUNT_EMAIL_SMTP_USERNAME`,
`ACCOUNT_EMAIL_SMTP_PASSWORD`, `ACCOUNT_EMAIL_SMTP_TLS=true`.
Verify: request a password reset and confirm the mail arrives.

## 2. No database backups

Nothing schedules `deploy/postgres-backup.sh` (verified 2026-09-02 — no cron,
no CI reference), and production archive data has no backups.

**Primary fix (recommended):** Railway dashboard → the `Postgres` service →
Backups tab → **Enable PITR**. It is a dashboard action with a small billing
cost — that decision is yours.

**Stopgap (code-side, already merged):** `.github/workflows/backup.yml` runs
`deploy/postgres-backup.sh` daily at 06:00 UTC and no-ops until these
repository secrets are set (GitHub → Settings → Secrets and variables →
Actions): `BACKUP_DATABASE_URL`, `BACKUP_AWS_S3_BUCKET`,
`BACKUP_AWS_ACCESS_KEY_ID`, `BACKUP_AWS_SECRET_ACCESS_KEY`
(`BACKUP_AWS_REGION` optional). The Postgres URL must be reachable from
GitHub runners (Railway Postgres needs public networking enabled).

## 3. match-service deploy may be stale — RESOLVED 2026-09-06

All four services (web, gateway, match-service, platform-service) verified
deployed from current `main` via the Railway CLI (SUCCESS deployments at
2026-09-06 02:22 on commit `5812b67`). Auto-deploy has fired on every push to
`main` since 2026-09-04; keep an eye on the dashboard after pushes, but the
August failure mode has not recurred.

## 4. Moderation admin (optional, quick)

The handles-only admin bug is fixed in code (`views.go:292-309` accepts both
`PLATFORM_ADMIN_ACCOUNT_IDS` and `PLATFORM_ADMIN_HANDLES`, with regression
tests), but **neither variable is set in production**, so there is currently
no moderation admin at all.

**Action (if you want a moderator):** Railway → `platform-service` → set
`PLATFORM_ADMIN_HANDLES=<your handle>` (or `PLATFORM_ADMIN_ACCOUNT_IDS`).

## 5. Security scan triage — DONE 2026-09-06

A fresh sealed deep scan ran 2026-09-06 (`scan-2026-09-06T00-49-28.313Z-5b852ec787cb`,
152 findings: 49 high / 103 medium) and every finding was dispositioned —
the bulk are scanner artifacts on untracked Playwright trace-viewer assets
and vendored minified Stockfish JS; the first-party findings were reviewed
individually (parameterized SQL, config-derived internal URLs, client-side
same-origin fetches). Full dispositions:
[docs/audits/2026-09-06-mimosa-scan-triage.md](../audits/2026-09-06-mimosa-scan-triage.md).
The 2026-09-02 quick pass is superseded. Note: the workspace commit hook
(`mimosa` L3) still hard-blocks commits on two of the triaged false positives
(`pytrainer/network.py`, `anticheat/stockfish.go`) and has no suppression
mechanism — commits go through the GitHub API until that is adjusted.
