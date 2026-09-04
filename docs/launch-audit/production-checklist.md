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

## 3. match-service deploy may be stale

The 2026-08-24 audit found `match-service` still on `a8ae5bc` (missing the
hosted-queue seat-secret fix) with two FAILED builds, while the rest of the
project was on `74da367`. Railway auto-deploy does not reliably fire on push
— every deploy in that pass had to be triggered by re-pointing the service
source.

**Action:** Railway dashboard → `match-service` → Deployments → confirm the
running deployment matches the current `main` SHA after this PR merges;
trigger a deploy manually and watch the build if it lags.

## 4. Moderation admin (optional, quick)

The handles-only admin bug is fixed in code (`views.go:292-309` accepts both
`PLATFORM_ADMIN_ACCOUNT_IDS` and `PLATFORM_ADMIN_HANDLES`, with regression
tests), but **neither variable is set in production**, so there is currently
no moderation admin at all.

**Action (if you want a moderator):** Railway → `platform-service` → set
`PLATFORM_ADMIN_HANDLES=<your handle>` (or `PLATFORM_ADMIN_ACCOUNT_IDS`).

## 5. Security scan report exists; triage still pending

A full mimosa scan ran 2026-09-02 (sealed, 43 findings: 16 high / 27 medium).
A quick pass judged the highs to be false positives on local dev tooling and
third-party minified Stockfish JS (report:
`~/.mimosa/security-scans/project-1d379e255e279db4efa9c218/scan-2026-09-02T20-24-23.402Z-9e1bf79c8efe/report.md`),
but a deliberate pre-launch triage pass has **not** been done. Do not treat
the scanner's silence (or this note) as evidence the platform is secure.
