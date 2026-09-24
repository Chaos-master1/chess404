'use client';

import React from 'react';
import { OFFICIAL_MATCH_MODES } from '@chess404/contracts';
import type { MatchModeId } from '@chess404/contracts';
import type { AccountLeaderboardSummary, AccountLeaderboardSpotlight, AccountProfile, AccountSeasonSummary, SeasonOption } from './lib/platform-service';
import { formatLastSeenLabel, formatRatingDelta } from './lib/display';
import { fetchAccountLeaderboard } from './lib/platform-service';

interface RankingsPageProps {
  onViewGuest?: (guestId: string) => void;
  onViewAccount?: (handle: string) => void;
}

function resolveDisplayedSeason(account: AccountProfile): AccountSeasonSummary | undefined {
  return account.selectedSeason ?? account.currentSeason;
}

function describeSeason(summary?: AccountSeasonSummary): string {
  if (!summary) {
    return 'No season matches yet';
  }
  return `${summary.label}: ${summary.matchesPlayed} matches, ${formatRatingDelta(summary.netDelta)}`;
}

function parseModeFilterValue(value: string): MatchModeId | '' {
  return OFFICIAL_MATCH_MODES.some((mode) => mode.id === value as MatchModeId) ? (value as MatchModeId) : '';
}

function formatWinRate(spotlight?: AccountLeaderboardSpotlight): string {
  if (!spotlight || spotlight.matchesPlayed <= 0) {
    return '--';
  }
  const winRate = Math.round((spotlight.wins / spotlight.matchesPlayed) * 100);
  return `${winRate}%`;
}

function renderSpotlightLabel(summary: AccountLeaderboardSummary | undefined, selectedModeId: MatchModeId | ''): string {
  if (summary?.seasonLabel?.trim()) {
    return summary.seasonLabel;
  }
  if (selectedModeId) {
    return `${OFFICIAL_MATCH_MODES.find((mode) => mode.id === selectedModeId)?.label ?? 'Mode'} ladder`;
  }
  return 'Current ladder';
}

function describeSparseLane(selectedModeId: MatchModeId | '', selectedSeasonId: string): string {
  if (selectedModeId || selectedSeasonId) {
    return 'No claimed account has posted rated results for this exact lane yet.';
  }
  return 'No claimed accounts are on the board yet. Open Account, claim a handle, and play your first rated match to seed the ladder.';
}

export default function RankingsPage({ onViewGuest, onViewAccount }: RankingsPageProps): React.ReactElement {
  const [accounts, setAccounts] = React.useState<AccountProfile[]>([]);
  const [seasons, setSeasons] = React.useState<SeasonOption[]>([]);
  const [summary, setSummary] = React.useState<AccountLeaderboardSummary | undefined>(undefined);
  const [selectedSeasonId, setSelectedSeasonId] = React.useState('');
  const [selectedModeId, setSelectedModeId] = React.useState<MatchModeId | ''>('');
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState('');

  const loadRankings = React.useCallback(async (seasonId?: string, modeId?: MatchModeId) => {
    setLoading(true);
    setError('');
    setSummary(undefined);
    try {
      const payload = await fetchAccountLeaderboard(50, 'rating', seasonId, modeId);
      setAccounts(payload.accounts);
      setSeasons(payload.seasons);
      setSummary(payload.summary);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load rankings.');
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void loadRankings(selectedSeasonId || undefined, selectedModeId || undefined);
  }, [loadRankings, selectedModeId, selectedSeasonId]);

  return (
    <div className="rankings-page">
      <style>{`
        .rankings-page {
          display: flex; flex: 1; min-height: 0; padding: 22px 28px 26px; gap: 18px;
        }
        .rankings-card {
          flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column;
          padding: 0; overflow: hidden;
        }
        .rankings-header {
          padding: 18px 20px 14px; border-bottom: 1px solid rgba(255,165,40,0.12);
        }
        .rankings-header-inner {
          display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap;
        }
        .rankings-filters {
          display: flex; gap: 10px; align-items: center; flex-wrap: wrap;
        }
        .rankings-filter-select {
          min-height: 40px; padding: 9px 12px; border-radius: 10px;
          border: 1px solid rgba(255,180,60,0.3); background: #121824;
          color: #fff4d6; color-scheme: dark; font-size: 12px; font-weight: 700;
          outline: none; cursor: pointer;
        }
        .rankings-body {
          flex: 1; min-height: 0; overflow-y: auto; padding: 20px;
        }
        .rankings-spotlight-grid {
          display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px;
        }
        .rankings-row {
          display: grid;
          grid-template-columns: 52px minmax(0, 1fr) 100px 180px 90px;
          gap: 12px; align-items: center; padding: 14px 16px; border-radius: 12px;
        }
        .rankings-row__rank { font-size: 18px; font-weight: 900; }
        .rankings-row__name { min-width: 0; }
        .rankings-row__elo { font-size: 15px; font-weight: 800; text-align: right; color: #7ce3aa; }
        .rankings-row__season { font-size: 11px; text-align: right; color: rgba(255,232,180,0.62); }
        .rankings-row__action { display: flex; justify-content: flex-end; }

        @media (max-width: 900px) {
          .rankings-page { padding: 14px 12px 18px; }
          .rankings-header { padding: 14px 14px 12px; }
          .rankings-filters { width: 100%; }
          .rankings-filter-select { flex: 1; min-width: 0; }
          .rankings-spotlight-grid {
            grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
          }
          .rankings-row {
            grid-template-columns: 40px 1fr auto;
            grid-template-rows: auto auto;
            gap: 6px 10px; padding: 12px 14px;
          }
          .rankings-row__rank { grid-row: 1 / 3; align-self: center; font-size: 16px; }
          .rankings-row__name { grid-column: 2; grid-row: 1; }
          .rankings-row__elo { grid-column: 3; grid-row: 1; text-align: right; font-size: 14px; }
          .rankings-row__season { grid-column: 2 / 4; grid-row: 2; text-align: left; }
          .rankings-row__action { display: none; }
        }

        @media (max-width: 480px) {
          .rankings-page { padding: 8px 6px 12px; }
          .rankings-header { padding: 12px 10px 10px; }
          .rankings-body { padding: 12px 8px; }
          .rankings-spotlight-grid {
            grid-template-columns: 1fr 1fr; gap: 8px;
          }
          .rankings-row { padding: 10px 10px; gap: 4px 8px; }
          .rankings-row__rank { font-size: 14px; }
        }
      `}</style>

      <div className="stat-card rankings-card">
        <div className="rankings-header">
          <div className="rankings-header-inner">
            <div>
              <div style={{ color: '#ffcf72', fontSize: '13px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase' }}>Account Rankings</div>
              <div style={{ color: 'rgba(255,232,180,0.72)', fontSize: '12px', marginTop: '4px' }}>
                Track the strongest claimed accounts by official mode, season, and rated form.
              </div>
            </div>
            <div className="rankings-filters">
              <select
                aria-label="Filter by mode"
                className="rankings-filter-select"
                value={selectedModeId}
                onChange={(event) => setSelectedModeId(parseModeFilterValue(event.target.value))}
              >
                <option value="" style={{ background: '#121824', color: '#fff4d6' }}>All official modes</option>
                {OFFICIAL_MATCH_MODES.filter((mode) => mode.id !== 'computer').map((mode) => (
                  <option key={mode.id} value={mode.id} style={{ background: '#121824', color: '#fff4d6' }}>
                    {mode.label}
                  </option>
                ))}
              </select>
              <select
                aria-label="Filter by season"
                className="rankings-filter-select"
                value={selectedSeasonId}
                onChange={(event) => setSelectedSeasonId(event.target.value)}
              >
                <option value="" style={{ background: '#121824', color: '#fff4d6' }}>All seasons</option>
                {seasons.map((season) => (
                  <option key={season.seasonId} value={season.seasonId} style={{ background: '#121824', color: '#fff4d6' }}>
                    {season.label}
                  </option>
                ))}
              </select>
              <button
                className="btn-primary"
                onClick={() => void loadRankings(selectedSeasonId || undefined, selectedModeId || undefined)}
                style={{ minHeight: '40px', padding: '9px 16px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center' }}
              >
                Refresh
              </button>
            </div>
          </div>
        </div>

        <div className="rankings-body">
          {!loading && summary && accounts.length > 1 && (
            <div style={{ display: 'grid', gap: '12px', marginBottom: '18px' }}>
              <div className="rankings-spotlight-grid">
                <div style={{ padding: '14px 16px', borderRadius: '12px', background: 'rgba(255,180,60,0.08)', border: '1px solid rgba(255,180,60,0.16)' }}>
                  <div style={{ color: '#ffcf72', fontSize: '11px', fontWeight: 800, letterSpacing: '0.9px', textTransform: 'uppercase' }}>{renderSpotlightLabel(summary, selectedModeId)}</div>
                  <div style={{ color: '#fff4d2', fontSize: '20px', fontWeight: 900, marginTop: '8px' }}>{summary.playerCount}</div>
                  <div style={{ color: 'rgba(255,232,180,0.6)', fontSize: '11px', marginTop: '4px' }}>
                    players in this lane · {summary.matchCount} rated results tracked
                  </div>
                </div>
                {[
                  { label: 'Leader', spotlight: summary.leader, value: summary.leader ? `${summary.leader.rating}` : '--', detail: summary.leader ? `${summary.leader.matchesPlayed} matches · ${formatWinRate(summary.leader)} win rate` : 'No leader yet' },
                  { label: 'Biggest climb', spotlight: summary.biggestClimber, value: summary.biggestClimber ? formatRatingDelta(summary.biggestClimber.netDelta) : '--', detail: summary.biggestClimber ? `${summary.biggestClimber.matchesPlayed} matches · rating ${summary.biggestClimber.rating}` : 'No climb data yet' },
                  { label: 'Peak holder', spotlight: summary.highestPeak, value: summary.highestPeak ? `${summary.highestPeak.peakRating}` : '--', detail: summary.highestPeak ? `${summary.highestPeak.matchesPlayed} matches · ${summary.highestPeak.displayName}` : 'No peak yet' },
                  { label: 'Most active', spotlight: summary.mostActive, value: summary.mostActive ? `${summary.mostActive.matchesPlayed}` : '--', detail: summary.mostActive ? `${summary.mostActive.displayName} · ${formatWinRate(summary.mostActive)} win rate` : 'No volume yet' },
                ].map((card) => (
                  <div key={card.label} style={{ padding: '14px 16px', borderRadius: '12px', background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,165,40,0.1)' }}>
                    <div style={{ color: '#ffcf72', fontSize: '11px', fontWeight: 800, letterSpacing: '0.9px', textTransform: 'uppercase' }}>{card.label}</div>
                    <div style={{ color: '#fff4d2', fontSize: '18px', fontWeight: 900, marginTop: '8px' }}>{card.value}</div>
                    <div style={{ color: '#ffd98f', fontSize: '12px', fontWeight: 700, marginTop: '6px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {card.spotlight ? `@${card.spotlight.handle}` : 'Waiting for results'}
                    </div>
                    <div style={{ color: 'rgba(255,232,180,0.58)', fontSize: '11px', marginTop: '4px', lineHeight: 1.45 }}>
                      {card.detail}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {!loading && summary && accounts.length === 1 && (
            <div
              style={{
                marginBottom: '18px',
                padding: '16px 18px',
                borderRadius: '14px',
                background: 'linear-gradient(180deg, rgba(200,134,10,0.16) 0%, rgba(70,42,8,0.22) 100%)',
                border: '1px solid rgba(255,180,60,0.18)',
              }}
            >
              <div style={{ color: '#ffcf72', fontSize: '11px', fontWeight: 800, letterSpacing: '0.9px', textTransform: 'uppercase' }}>
                First account on this lane
              </div>
              <div style={{ color: '#fff4d2', fontSize: '20px', fontWeight: 900, marginTop: '8px' }}>
                @{accounts[0]?.handle} sets the first benchmark
              </div>
              <div style={{ color: 'rgba(255,232,180,0.7)', fontSize: '12px', lineHeight: 1.6, marginTop: '6px' }}>
                {renderSpotlightLabel(summary, selectedModeId)} has only one claimed competitor right now. More rated results will unlock richer ladder comparisons and momentum cards.
              </div>
            </div>
          )}

          {error && (
            <div style={{
              marginBottom: '16px',
              padding: '12px 14px',
              borderRadius: '10px',
              background: 'rgba(120,20,20,0.22)',
              border: '1px solid rgba(231,76,60,0.32)',
              color: '#ffb1a7',
              fontSize: '12px',
              fontWeight: 700,
            }}>
              {error}
            </div>
          )}

          {loading ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }} aria-busy="true" aria-label="Loading rankings">
              {[1, 2, 3, 4, 5].map((i) => (
                <div
                  key={i}
                  style={{
                    height: '58px',
                    borderRadius: '12px',
                    background: 'linear-gradient(90deg, rgba(255,255,255,0.03) 0%, rgba(255,255,255,0.08) 50%, rgba(255,255,255,0.03) 100%)',
                    backgroundSize: '200% 100%',
                    animation: 'shimmer 1.8s infinite',
                    border: '1px solid rgba(255,180,60,0.1)',
                  }}
                />
              ))}
            </div>
          ) : accounts.length === 0 ? (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                textAlign: 'center',
                padding: '36px 20px',
                borderRadius: '16px',
                background: 'linear-gradient(180deg, rgba(255,255,255,0.04) 0%, rgba(255,255,255,0.02) 100%)',
                border: '1px solid rgba(255,165,40,0.15)',
                color: 'rgba(255,232,180,0.72)',
                fontSize: '13px',
                lineHeight: 1.65,
                gap: '8px',
              }}
            >
              <div style={{ fontSize: '32px', marginBottom: '4px' }}>🏆</div>
              <div style={{ fontSize: '15px', fontWeight: 800, color: '#ffd487' }}>No Ranked Players Yet</div>
              <div style={{ maxWidth: '420px', color: 'rgba(255,232,180,0.65)' }}>
                {describeSparseLane(selectedModeId, selectedSeasonId)}
              </div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {accounts.map((account, index) => {
                const season = resolveDisplayedSeason(account);
                return (
                  <div
                    key={account.accountId}
                    className="table-row rankings-row"
                    onClick={() => {
                      if (onViewAccount) {
                        onViewAccount(account.handle);
                        return;
                      }
                      onViewGuest?.(account.primaryGuestId);
                    }}
                    style={{
                      background: index < 3
                        ? 'linear-gradient(180deg, rgba(200,134,10,0.18) 0%, rgba(70,42,8,0.22) 100%)'
                        : 'rgba(255,255,255,0.03)',
                      border: index < 3
                        ? '1px solid rgba(255,180,60,0.18)'
                        : '1px solid rgba(255,165,40,0.08)',
                      cursor: 'pointer',
                    }}
                  >
                    <div className="rankings-row__rank" style={{ color: index === 0 ? '#ffd76e' : index === 1 ? '#d9e0ef' : index === 2 ? '#d79d72' : 'rgba(255,232,180,0.7)' }}>
                      #{index + 1}
                    </div>
                    <div className="rankings-row__name">
                      <div style={{ color: '#fff2c8', fontSize: '14px', fontWeight: 800, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {account.displayName ?? account.handle}
                      </div>
                      <div style={{ color: '#ffd98f', fontSize: '11px', fontWeight: 700, marginTop: '3px' }}>@{account.handle}</div>
                      <div style={{ color: 'rgba(170,190,220,0.62)', fontSize: '11px', marginTop: '4px' }}>
                        {account.matchesPlayed ?? 0} matches - {account.wins ?? 0}W {account.losses ?? 0}L {account.draws ?? 0}D - {account.guestCount ?? account.linkedGuestIds.length} guest{(account.guestCount ?? account.linkedGuestIds.length) === 1 ? '' : 's'}
                      </div>
                      <div style={{ color: 'rgba(255,232,180,0.56)', fontSize: '11px', marginTop: '4px' }}>
                        {describeSeason(season)}
                      </div>
                    </div>
                    <div className="rankings-row__elo">
                      {selectedSeasonId && season ? season.ratingEnd : (account.rating ?? 1200)} Elo
                    </div>
                    <div className="rankings-row__season">
                      {season
                        ? `${season.label} peak ${season.peakRating}`
                        : formatLastSeenLabel(account.lastSeenAt)}
                    </div>
                    <div className="rankings-row__action">
                      <button
                        className="btn-secondary"
                        onClick={(e) => {
                          e.stopPropagation();
                          if (onViewAccount) {
                            onViewAccount(account.handle);
                            return;
                          }
                          onViewGuest?.(account.primaryGuestId);
                        }}
                        style={{ padding: '7px 10px', fontSize: '11px' }}
                      >
                        Profile
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
