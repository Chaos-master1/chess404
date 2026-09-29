'use client';

import React from 'react';
import type { AccountProfile, MatchArchiveEntry } from './lib/platform-service';
import { fetchAccountArchivedMatches, fetchAccounts } from './lib/platform-service';
import { formatDateTime } from './lib/display';

interface CommunityPageProps {
  onOpenMatch?: (matchId: string) => void;
  onOpenAccount?: (handle: string) => void;
}

type MatchOutcome = 'win' | 'loss' | 'draw' | 'active';

type Presence = 'online' | 'recently_active' | 'offline';

function presenceOf(account: AccountProfile): Presence {
  return account.presenceStatus ?? 'offline';
}

const PRESENCE_META: Record<Presence, { label: string; color: string }> = {
  online: { label: 'Online now', color: '#7ce3aa' },
  recently_active: { label: 'Recently active', color: '#ffd487' },
  offline: { label: 'Offline', color: 'rgba(170,190,220,0.5)' },
};

// Guests are anonymous sessions: they have no handle and no meaningful Elo
// until they finish rated games. Accounts without rated games are shown as
// Unrated instead of surfacing the default 1200 ladder seed.
function ratingLabel(account: AccountProfile): string {
  const rating = account.modeRating ?? account.rating;
  const played = account.matchesPlayed ?? 0;
  if (!rating || played <= 0) {
    return 'Unrated';
  }
  return String(rating);
}

function hasRecord(account: AccountProfile): boolean {
  return (account.matchesPlayed ?? 0) > 0;
}

function recordLabel(account: AccountProfile): string {
  return `${account.wins ?? 0}W ${account.losses ?? 0}L ${account.draws ?? 0}D`;
}

function describeAccountMatch(match: MatchArchiveEntry, accountId: string): { opponent: string; result: MatchOutcome } {
  const isWhite = match.whiteAccountId === accountId;
  const opponentBase = isWhite
    ? (match.blackAccountHandle ? `@${match.blackAccountHandle}` : (match.blackName ?? 'Guest'))
    : (match.whiteAccountHandle ? `@${match.whiteAccountHandle}` : (match.whiteName ?? 'Guest'));
  const result: MatchOutcome =
    match.winner === 'draw'
      ? 'draw'
      : match.winner === (isWhite ? 'white' : 'black')
        ? 'win'
        : match.status === 'finished'
          ? 'loss'
          : 'active';
  return { opponent: opponentBase, result };
}

const OUTCOME_LABEL: Record<MatchOutcome, string> = {
  win: 'Win',
  loss: 'Loss',
  draw: 'Draw',
  active: 'In progress',
};

export default function CommunityPage({
  onOpenMatch,
  onOpenAccount,
}: CommunityPageProps): React.ReactElement {
  const [accounts, setAccounts] = React.useState<AccountProfile[]>([]);
  const [selectedAccountId, setSelectedAccountId] = React.useState<string | null>(null);
  const [selectedAccount, setSelectedAccount] = React.useState<AccountProfile | null>(null);
  const [recentMatches, setRecentMatches] = React.useState<MatchArchiveEntry[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [loadingMatches, setLoadingMatches] = React.useState(false);
  const [listError, setListError] = React.useState('');
  const [matchesError, setMatchesError] = React.useState('');

  const loadAccounts = React.useCallback(async () => {
    setLoading(true);
    setListError('');
    try {
      const nextAccounts = await fetchAccounts(50, 'rating');
      setAccounts(nextAccounts);
      setSelectedAccountId(current => current && nextAccounts.some(account => account.accountId === current)
        ? current
        : nextAccounts[0]?.accountId ?? null);
    } catch (err) {
      setListError(err instanceof Error ? err.message : 'Failed to load community players.');
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

  React.useEffect(() => {
    if (!selectedAccountId) {
      setSelectedAccount(null);
      setRecentMatches([]);
      return;
    }

    setMatchesError('');

    const known = accounts.find(account => account.accountId === selectedAccountId);
    if (known) {
      setSelectedAccount(known);
    }

    let cancelled = false;
    setLoadingMatches(true);

    void fetchAccountArchivedMatches(selectedAccountId, 8)
      .then(matches => {
        if (!cancelled) {
          setRecentMatches(matches);
        }
      })
      .catch(err => {
        if (!cancelled) {
          setMatchesError(err instanceof Error ? err.message : 'Failed to load match history.');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingMatches(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedAccountId, accounts]);

  const featuredAccount = selectedAccount ?? accounts.find(account => account.accountId === selectedAccountId) ?? null;
  const featuredPresence = featuredAccount ? presenceOf(featuredAccount) : null;

  return (
    <div className="community-page">
      <style>{`
        .community-page {
          display: flex; flex: 1; min-height: 0; padding: 22px 28px 26px; gap: 18px;
        }
        .community-pane {
          min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow: hidden;
        }
        .community-list-pane { width: min(380px, 36%); flex-shrink: 0; }
        .community-detail-pane { flex: 1; }
        .community-pane-header {
          display: flex; justify-content: space-between; align-items: center; gap: 12px;
          padding: 18px 20px 14px; border-bottom: 1px solid rgba(255,165,40,0.12);
        }
        .community-pane-title {
          color: #ffcf72; font-size: 13px; font-weight: 800;
          letter-spacing: 1.2px; text-transform: uppercase;
        }
        .community-pane-sub { color: var(--text-subtle, rgba(180,194,220,0.55)); font-size: 12px; margin-top: 4px; }
        .community-body { flex: 1; min-height: 0; overflow-y: auto; padding: 20px; }
        .community-alert {
          margin-bottom: 16px; padding: 12px 14px; border-radius: 10px;
          background: rgba(120,20,20,0.22); border: 1px solid rgba(231,76,60,0.32);
          color: #ffb1a7; font-size: 12px; font-weight: 700;
        }
        .community-skeleton {
          height: 68px; border-radius: 12px; border: 1px solid rgba(255,165,40,0.1);
          background: linear-gradient(90deg, rgba(255,255,255,0.03) 0%, rgba(255,255,255,0.08) 50%, rgba(255,255,255,0.03) 100%);
          background-size: 200% 100%;
        }
        @media (prefers-reduced-motion: no-preference) {
          .community-skeleton { animation: community-shimmer 1.8s infinite; }
          @keyframes community-shimmer {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
          }
        }
        .community-empty {
          display: grid; place-items: center; text-align: center; gap: 8px;
          padding: 36px 20px; border-radius: 14px;
          border: 1px dashed rgba(255,190,90,0.22); background: rgba(255,255,255,0.02);
          color: var(--text-subtle, rgba(180,194,220,0.55)); font-size: 13px;
        }
        .community-empty__icon { font-size: 30px; }
        .community-empty__title { color: #ffd487; font-size: 15px; font-weight: 800; }
        .community-account-list { display: flex; flex-direction: column; gap: 10px; }
        .community-account-card {
          cursor: pointer; text-align: left; border-radius: 12px; padding: 13px 14px;
          border: 1px solid rgba(255,165,40,0.12); color: inherit; width: 100%;
          background: linear-gradient(180deg, rgba(255,255,255,0.04) 0%, rgba(255,255,255,0.02) 100%);
          transition: border-color var(--transition-fast, 120ms), background var(--transition-fast, 120ms);
        }
        .community-account-card:hover { border-color: rgba(255,190,90,0.35); }
        .community-account-card--selected {
          border-color: rgba(255,190,90,0.32);
          background: linear-gradient(180deg, rgba(200,134,10,0.18) 0%, rgba(70,42,8,0.2) 100%);
        }
        .community-account-card__top { display: flex; justify-content: space-between; gap: 12px; align-items: baseline; }
        .community-account-card__handle {
          color: #fff2c8; font-size: 14px; font-weight: 800; min-width: 0;
          white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
          display: inline-flex; align-items: center; gap: 8px;
        }
        .community-account-card__rating { color: #7ce3aa; font-size: 15px; font-weight: 800; flex-shrink: 0; }
        .community-account-card__rating--unrated { color: rgba(170,190,220,0.55); font-size: 12px; font-weight: 700; }
        .community-account-card__meta {
          margin-top: 5px; color: rgba(170,190,220,0.62); font-size: 11px;
          white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
        }
        .community-presence-dot {
          width: 8px; height: 8px; border-radius: 999px; flex-shrink: 0; display: inline-block;
        }
        .community-presence-dot--online { background: #4ade80; box-shadow: 0 0 6px rgba(74,222,128,0.8); }
        .community-presence-dot--recently_active { background: #fbbf24; }
        .community-presence-dot--offline { background: rgba(140,160,190,0.4); }
        .community-detail-header-card {
          padding: 18px; border-radius: 14px;
          background: linear-gradient(180deg, rgba(200,134,10,0.16) 0%, rgba(70,42,8,0.2) 100%);
          border: 1px solid rgba(255,185,70,0.18);
        }
        .community-detail-header-card__top { display: flex; justify-content: space-between; gap: 14px; align-items: flex-start; }
        .community-detail-header-card__name { color: #fff2c8; font-size: 21px; font-weight: 900; }
        .community-detail-header-card__meta { color: rgba(255,232,180,0.62); font-size: 12px; margin-top: 5px; }
        .community-detail-header-card__rating { color: #7ce3aa; font-size: 26px; font-weight: 900; flex-shrink: 0; }
        .community-detail-header-card__rating-label {
          display: block; text-align: right; color: rgba(170,190,220,0.55);
          font-size: 10px; font-weight: 700; letter-spacing: 0.8px; text-transform: uppercase; margin-top: 4px;
        }
        .community-detail-actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 14px; }
        .community-stat-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
        .community-stat-tile {
          padding: 14px 14px 12px; border-radius: 12px;
          background: rgba(255,255,255,0.035); border: 1px solid rgba(255,165,40,0.08);
        }
        .community-stat-tile__label {
          color: rgba(255,232,180,0.58); font-size: 10px; font-weight: 700;
          letter-spacing: 0.8px; text-transform: uppercase;
        }
        .community-stat-tile__value { font-size: 20px; font-weight: 900; margin-top: 6px; }
        .community-stat-tile__hint { color: rgba(170,190,220,0.55); font-size: 11px; margin-top: 4px; }
        .community-guests-note {
          padding: 12px 16px; border-radius: 12px;
          background: rgba(255,255,255,0.03); border: 1px solid rgba(255,165,40,0.08);
          color: rgba(255,232,180,0.72); font-size: 12px;
        }
        .community-matches-card {
          padding: 16px; border-radius: 14px;
          background: rgba(255,255,255,0.03); border: 1px solid rgba(255,165,40,0.08);
        }
        .community-matches-card__title {
          color: #ffcf72; font-size: 12px; font-weight: 800;
          text-transform: uppercase; letter-spacing: 1px;
        }
        .community-matches-card__loading { color: rgba(255,232,180,0.55); font-size: 11px; }
        .community-match-row {
          text-align: left; cursor: pointer; width: 100%;
          padding: 11px 12px; border-radius: 10px;
          border: 1px solid rgba(255,165,40,0.12);
          background: linear-gradient(180deg, rgba(18,23,36,0.95) 0%, rgba(11,14,24,0.94) 100%);
          color: #fff2c8;
          transition: border-color var(--transition-fast, 120ms);
        }
        .community-match-row:hover { border-color: rgba(255,190,90,0.3); }
        .community-match-row__top { display: flex; justify-content: space-between; gap: 10px; align-items: center; }
        .community-match-row__opponent { font-size: 12px; font-weight: 800; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .community-match-row__meta { margin-top: 5px; color: rgba(170,190,220,0.62); font-size: 11px; }
        .community-match-row--win { border-left: 3px solid rgba(124,227,170,0.7); }
        .community-match-row--loss { border-left: 3px solid rgba(255,120,100,0.6); }
        .community-match-row--draw { border-left: 3px solid rgba(255,215,130,0.5); }
        .community-match-row--active { border-left: 3px solid rgba(120,170,255,0.6); }
        .community-match-row__result--win { color: #8ef0b6; }
        .community-match-row__result--loss { color: #ffb3a0; }
        .community-match-row__result--draw { color: #ffe2a5; }
        .community-match-row__result--active { color: #a9c9ff; }
        .community-stack { display: flex; flex-direction: column; gap: 16px; }
        .community-detail-empty { color: rgba(255,232,180,0.65); font-size: 13px; }

        @media (max-width: 1024px) {
          .community-page { flex-direction: column; padding: 18px 18px 96px; }
          .community-list-pane { width: 100%; flex-shrink: 1; }
          .community-account-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); }
        }
        @media (max-width: 640px) {
          .community-page { padding: 12px 10px 18px; gap: 12px; }
          .community-pane-header { padding: 14px 14px 12px; }
          .community-body { padding: 12px 10px; }
          .community-account-list { grid-template-columns: 1fr; }
          .community-stat-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
          .community-detail-header-card { padding: 14px; }
        }
      `}</style>

      <div className="card-surface community-pane community-list-pane">
        <div className="community-pane-header">
          <div style={{ minWidth: 0 }}>
            <div className="community-pane-title">Community</div>
            <div className="community-pane-sub">Registered players on the platform.</div>
          </div>
          <button className="btn-ghost" onClick={() => void loadAccounts()}>
            Refresh
          </button>
        </div>

        <div className="community-body">
          {listError && <div className="community-alert" role="alert">{listError}</div>}

          {loading ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }} aria-busy="true" aria-label="Loading community players">
              {[1, 2, 3, 4, 5].map(i => <div key={i} className="community-skeleton" />)}
            </div>
          ) : accounts.length === 0 ? (
            <div className="community-empty">
              <div className="community-empty__icon">👥</div>
              <div className="community-empty__title">No players yet</div>
              <div style={{ maxWidth: '300px' }}>
                Create an account to appear on the community board.
              </div>
            </div>
          ) : (
            <div className="community-account-list">
              {accounts.map(account => {
                const presence = presenceOf(account);
                const rated = hasRecord(account);
                return (
                  <button
                    key={account.accountId}
                    onClick={() => setSelectedAccountId(account.accountId)}
                    className={`community-account-card${selectedAccountId === account.accountId ? ' community-account-card--selected' : ''}`}
                  >
                    <div className="community-account-card__top">
                      <span className="community-account-card__handle">
                        <span className={`community-presence-dot community-presence-dot--${presence}`} aria-hidden="true" />
                        @{account.handle}
                      </span>
                      <span className={`community-account-card__rating${rated ? '' : ' community-account-card__rating--unrated'}`}>
                        {ratingLabel(account)}
                      </span>
                    </div>
                    <div className="community-account-card__meta">
                      {rated
                        ? `${recordLabel(account)} · ${account.matchesPlayed} ${(account.matchesPlayed ?? 0) === 1 ? 'match' : 'matches'} · ${PRESENCE_META[presence].label}`
                        : PRESENCE_META[presence].label}
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      <div className="card-surface community-pane community-detail-pane">
        <div className="community-pane-header">
          <div style={{ minWidth: 0 }}>
            <div className="community-pane-title">Player Profile</div>
            <div className="community-pane-sub">Stats and recent matches for the selected player.</div>
          </div>
        </div>

        <div className="community-body">
          {matchesError && <div className="community-alert" role="alert">{matchesError}</div>}

          {!featuredAccount ? (
            <div className="community-detail-empty">Select a player from the list to see their profile.</div>
          ) : (
            <div className="community-stack">
              <div className="community-detail-header-card">
                <div className="community-detail-header-card__top">
                  <div style={{ minWidth: 0 }}>
                    <div className="community-detail-header-card__name">@{featuredAccount.handle}</div>
                    <div className="community-detail-header-card__meta">
                      <span style={{ color: PRESENCE_META[featuredPresence ?? 'offline'].color, fontWeight: 700 }}>
                        {PRESENCE_META[featuredPresence ?? 'offline'].label}
                      </span>
                      {' · joined '}
                      {formatDateTime(featuredAccount.createdAt)}
                    </div>
                  </div>
                  <div>
                    <span className="community-detail-header-card__rating">{ratingLabel(featuredAccount)}</span>
                    <span className="community-detail-header-card__rating-label">Ladder</span>
                  </div>
                </div>

                <div className="community-detail-actions">
                  <button className="btn-ghost" onClick={() => onOpenAccount?.(featuredAccount.handle)} disabled={!onOpenAccount}>
                    Open Full Profile
                  </button>
                </div>
              </div>

              {hasRecord(featuredAccount) ? (
                (() => {
                  const played = featuredAccount.matchesPlayed ?? 0;
                  const winRate = played > 0 ? Math.round(((featuredAccount.wins ?? 0) / played) * 100) : null;
                  const stats: Array<{ label: string; value: number | string; color: string; hint?: string }> = [
                    { label: 'Matches', value: played, color: '#d8eaff', hint: winRate !== null ? `${winRate}% won` : undefined },
                    { label: 'Wins', value: featuredAccount.wins ?? 0, color: '#8ef0b6' },
                    { label: 'Losses', value: featuredAccount.losses ?? 0, color: '#ffb3a0' },
                    { label: 'Draws', value: featuredAccount.draws ?? 0, color: '#ffe2a5' },
                  ];
                  return (
                    <div className="community-stat-grid">
                      {stats.map(stat => (
                        <div key={stat.label} className="community-stat-tile">
                          <div className="community-stat-tile__label">{stat.label}</div>
                          <div className="community-stat-tile__value" style={{ color: stat.color }}>{stat.value}</div>
                          {stat.hint && <div className="community-stat-tile__hint">{stat.hint}</div>}
                        </div>
                      ))}
                    </div>
                  );
                })()
              ) : (
                <div className="community-empty" style={{ padding: '24px 16px' }}>
                  <div className="community-empty__title">No ranked games yet</div>
                  <div>This player hasn&apos;t finished a rated match, so there are no stats to show.</div>
                </div>
              )}

              {(featuredAccount.guestCount ?? 0) > 0 && (
                <div className="community-guests-note">
                  Also has {featuredAccount.guestCount} linked guest session{(featuredAccount.guestCount ?? 0) === 1 ? '' : 's'} from before claiming a handle.
                </div>
              )}

              <div className="community-matches-card">
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '10px', alignItems: 'center' }}>
                  <div className="community-matches-card__title">Recent Matches</div>
                  {loadingMatches && <div className="community-matches-card__loading">Loading…</div>}
                </div>

                {!hasRecord(featuredAccount) ? (
                  <div className="community-detail-empty" style={{ marginTop: '12px' }}>
                    No matches to show yet.
                  </div>
                ) : recentMatches.length === 0 ? (
                  <div className="community-detail-empty" style={{ marginTop: '12px' }}>
                    {loadingMatches ? 'Looking up archived matches…' : 'No archived matches yet for this player.'}
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginTop: '12px' }}>
                    {recentMatches.map(match => {
                      const info = describeAccountMatch(match, featuredAccount.accountId);
                      return (
                        <button
                          key={match.matchId}
                          onClick={() => onOpenMatch?.(match.matchId)}
                          className={`community-match-row community-match-row--${info.result}`}
                          disabled={!onOpenMatch}
                        >
                          <div className="community-match-row__top">
                            <span className="community-match-row__opponent">vs {info.opponent}</span>
                            <span className={`community-match-row__result--${info.result}`} style={{ fontSize: '11px', fontWeight: 800, flexShrink: 0 }}>
                              {OUTCOME_LABEL[info.result]}
                            </span>
                          </div>
                          <div className="community-match-row__meta">
                            {match.moveCount} moves · {formatDateTime(match.updatedAt)}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
