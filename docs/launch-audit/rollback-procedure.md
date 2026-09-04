# Chess404 Rollback Procedure

Status: written and verified against Railway's actual documented behavior and
this project's live deployment history on 2026-08-11. **Not yet rehearsed as
a live drill** — see "What's not yet verified" at the end.

## The one thing to unlearn first

The obvious assumption — "if something breaks, click rollback in the Railway
dashboard" — is not reliably available. Checked live: every one of the
services' deployment history shows every deployment *before* the current one
as `REMOVED`. Per Railway's own docs, once a deployment is superseded it
starts counting down against the plan's **image retention window**, and
outside that window the one-click, instant "Rollback" option disappears
entirely:

| Plan | Retention window |
|---|---|
| Free / Trial | 24 hours |
| Hobby | 72 hours |
| Pro | 120 hours |
| Enterprise | 360 hours |

**Confirm this project's actual plan tier before relying on any specific
number above** — it wasn't independently confirmed via the API during this
pass, only inferred from context.

The good news: rollback capability doesn't actually disappear outside the
window, it just gets slower. Per Railway's docs verbatim: *"A removed
deployment that is outside of the retention policy will not have the option
to rollback; instead, you will need to use the redeploy feature. This will
rebuild the image from the original source code with the deployment's
original variables."* Railway keeps a permanent record of each deployment's
source commit and variables — "Redeploy" on an old deployment triggers a
fresh build from that record, not a manual git operation.

**So there are two paths, not one:**

| Situation | Action | Speed |
|---|---|---|
| Target deployment is within the retention window | **Rollback** | Instant — restores the exact prior Docker image + variables, no rebuild |
| Target deployment is older than the retention window | **Redeploy** (on that specific old deployment, not the generic "redeploy current") | Slower — a full rebuild from that deployment's recorded source + config |

## Step-by-step

1. **Identify the last known-good deployment** for each affected service.
   Cross-reference the commit SHA shown against `git log` on `main` to be
   sure it predates the bad change — don't assume "one deployment back" is
   automatically safe if multiple bad commits landed close together.

2. **Per affected service**, in the Railway dashboard: Service → Deployments
   tab → three-dot menu on the target deployment row → **Rollback** (if
   available) or **Redeploy** (if Rollback isn't offered).

   This is a **dashboard-only action right now** — the Railway MCP tooling
   available in this session (`redeploy`) only re-triggers a service's
   *current* deployment again; it has no parameter to target a specific past
   deployment ID. If a future session has a more capable Railway MCP tool or
   the GraphQL API directly, prefer that; until then, this step needs a human
   at the dashboard, or an agent driving a browser against it.

3. **Services deploy independently, not atomically.** Per Railway's own
   docs, a monorepo push deploys each connected service separately with no
   ordering between them (ordering only applies to batch operations like
   template deploys or staged variable changes — not a normal `git push`).
   If a bad change touched multiple services, **each one needs its own
   rollback action** — there's no single "roll back everything" button.

4. **Watch health after every rollback**, not just deploy status: hit each
   service's `/healthz` (`/readyz` for platform-service), and specifically
   re-check the things this pre-launch pass already fixed once — Redis
   connection stability, the `matchMap` shard behavior — since a rollback to
   an *older* image could reintroduce a bug that was already fixed forward.

5. **Database migrations do not roll back with the code.** `services/realtime/cmd/migrate`
   manages schema changes independently of service deploys. If the bad
   deploy included a migration, rolling back the service code alone leaves
   the newer schema in place, potentially against older code that doesn't
   expect it. Check `services/realtime/migrations/postgres/` for any
   migration that shipped with the commit being rolled back, and run its
   corresponding `.down.sql` **before** rolling back the code, not after.
   This is the single most common way a "rollback" makes an incident worse
   instead of better — code and schema must be rolled back in the right
   order, not just the code.

6. **Communicate the rollback** — this is a visible, shared-state action per
   this project's own safety conventions, not a silent fix.

## What's not yet verified

- **The exact plan tier and its retention window** — confirm before trusting
  the table above for real incident timing.
- **A live rehearsal.** Everything above is verified against Railway's
  documented behavior and this project's actual deployment history, but
  nobody has actually clicked through a real rollback on this project yet.
  The one thing worth a deliberate, low-risk test: pick one non-critical
  service, `git revert` a trivial no-op commit on `main`, let it auto-deploy,
  then perform an actual dashboard rollback (or redeploy) back to the prior
  state and confirm the service comes back healthy. This needs the
  dashboard-click step above, so it needs either a human driving it or an
  agent with browser access to the Railway dashboard — not something the
  current Railway MCP tool set can complete standalone.
