'use client';

import React from 'react';
import { useRouter } from 'next/navigation';
import { type MatchModeId, type PieceColor } from '@chess404/contracts';
import { createPrivateMatch, type PrivateMatchIdentity } from './lib/private-match-service';
import { writeStoredRoomMeta } from './lib/match-service';

interface ComputerPageProps {
  identity: PrivateMatchIdentity | null;
  embedded?: boolean;
}

type DifficultyValue = 'beginner' | 'easy' | 'medium' | 'hard' | 'expert';
type PlayerColorChoice = 'white' | 'random' | 'black';

const DIFFICULTIES: ReadonlyArray<{
  value: DifficultyValue;
  label: string;
  description: string;
}> = [
  { value: 'beginner', label: 'Beginner', description: 'Learns basics, makes occasional mistakes' },
  { value: 'easy', label: 'Easy', description: 'Solid fundamentals, predictable patterns' },
  { value: 'medium', label: 'Medium', description: 'Good tactical awareness, moderate depth' },
  { value: 'hard', label: 'Hard', description: 'Strong player, deep calculation' },
  { value: 'expert', label: 'Expert', description: 'Near-optimal play, full engine depth' },
];

const COLOR_CHOICES: ReadonlyArray<{
  value: PlayerColorChoice;
  label: string;
  piece: string;
  description: string;
}> = [
  { value: 'white', label: 'White', piece: '♔', description: 'Moves first' },
  { value: 'random', label: 'Random', piece: '🎲', description: '50% side' },
  { value: 'black', label: 'Black', piece: '♚', description: 'Moves second' },
];

const DEFAULT_DIFFICULTY: DifficultyValue = 'medium';

export default function ComputerPage({ identity, embedded = false }: ComputerPageProps): React.ReactElement {
  const router = useRouter();
  const [selectedDifficulty, setSelectedDifficulty] = React.useState<DifficultyValue>(DEFAULT_DIFFICULTY);
  const [selectedColor, setSelectedColor] = React.useState<PlayerColorChoice>('white');
  const [inflightDifficulty, setInflightDifficulty] = React.useState<DifficultyValue | null>(null);
  const [inflightColor, setInflightColor] = React.useState<PieceColor | null>(null);
  const [error, setError] = React.useState('');
  const [created, setCreated] = React.useState<{ matchId: string; seatColor: PieceColor } | null>(null);

  const createMatch = React.useCallback(async (difficulty: DifficultyValue, colorChoice: PlayerColorChoice): Promise<boolean> => {
    if (!identity?.guestId) {
      setError('Your hosted player session is still loading — try again in a moment.');
      return false;
    }
    if (inflightDifficulty !== null) {
      return false;
    }

    const resolvedSeat: PieceColor = colorChoice === 'random'
      ? (Math.random() < 0.5 ? 'white' : 'black')
      : colorChoice;

    setInflightDifficulty(difficulty);
    setInflightColor(resolvedSeat);
    setError('');
    try {
      const result = await createPrivateMatch({
        identity,
        queue: 'direct',
        modeId: 'computer' as MatchModeId,
        difficulty,
        clockSeconds: 600,
        preferredSeat: resolvedSeat,
      });
      writeStoredRoomMeta(result.matchId, {
        queue: 'direct',
        modeId: 'computer' as MatchModeId,
        clockSeconds: 600,
        difficulty,
        viewerSeat: result.seatColor,
        whiteGuestId: result.snapshot.match.whiteGuestId,
        blackGuestId: result.snapshot.match.blackGuestId,
        whiteAccountId: result.snapshot.match.whiteAccountId,
        blackAccountId: result.snapshot.match.blackAccountId,
        whiteName: result.snapshot.match.whiteName,
        blackName: result.snapshot.match.blackName,
        whitePlayerSecret: result.seatColor === 'white' ? result.claim?.playerSecret : undefined,
        blackPlayerSecret: result.seatColor === 'black' ? result.claim?.playerSecret : undefined,
        whiteClaimToken: result.seatColor === 'white' ? result.claim?.claimToken : undefined,
        blackClaimToken: result.seatColor === 'black' ? result.claim?.claimToken : undefined,
        whiteClaimExpiresAt: result.seatColor === 'white' ? result.claim?.expiresAt : undefined,
        blackClaimExpiresAt: result.seatColor === 'black' ? result.claim?.expiresAt : undefined,
      });
      setCreated({ matchId: result.matchId, seatColor: result.seatColor });
      router.push(`/match/${encodeURIComponent(result.matchId)}`);
      return true;
    } catch (err) {
      const raw = err instanceof Error ? err.message : 'Failed to create computer match.';
      const lower = raw.toLowerCase();
      if (lower.includes('unauthorized guest') || lower.includes('unknown guest') || lower.includes('unauthorized')) {
        setError('Your hosted player session expired. Please refresh the page to start a new game.');
      } else if (lower.includes('rate limit') || lower.includes('retry after')) {
        setError('Too many recent requests — wait a few seconds and try again.');
      } else if (lower.includes('context deadline') || lower.includes('match-service unreachable')) {
        setError('The match service is taking too long to respond. Try again in a moment.');
      } else {
        setError(raw);
      }
      return false;
    } finally {
      setInflightDifficulty(null);
      setInflightColor(null);
    }
  }, [identity, inflightDifficulty, router]);

  const handleDifficultyClick = React.useCallback((difficulty: DifficultyValue) => {
    setSelectedDifficulty(difficulty);
  }, []);

  const handleColorClick = React.useCallback((color: PlayerColorChoice) => {
    setSelectedColor(color);
  }, []);

  const handlePlay = React.useCallback(() => {
    void createMatch(selectedDifficulty, selectedColor);
  }, [createMatch, selectedDifficulty, selectedColor]);

  const openMatch = React.useCallback(() => {
    if (!created?.matchId) return;
    router.push(`/match/${encodeURIComponent(created.matchId)}`);
  }, [created?.matchId, router]);

  if (created) {
    const matchedDifficulty = DIFFICULTIES.find((d) => d.value === selectedDifficulty);
    const displayColor = created.seatColor === 'black' ? 'Black' : 'White';
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <div style={{
          padding: '14px',
          borderRadius: '12px',
          background: 'rgba(180,130,255,0.08)',
          border: '1px solid rgba(180,130,255,0.2)',
        }}>
          <div style={{ color: '#d4a0ff', fontSize: '14px', fontWeight: 800 }}>
            Match Ready
          </div>
          <div style={{ color: 'rgba(220,210,255,0.8)', fontSize: '13px', marginTop: '6px' }}>
            Your {matchedDifficulty?.label ?? 'Medium'} game against the computer is starting. You play as {displayColor}.
          </div>
        </div>
        <button
          onClick={openMatch}
          style={{
            padding: '12px 16px',
            borderRadius: '10px',
            border: '1px solid rgba(180,130,255,0.36)',
            background: 'linear-gradient(180deg, rgba(130,80,210,0.95) 0%, rgba(70,40,130,0.98) 100%)',
            color: '#f7fbff',
            fontWeight: 800,
            fontSize: '13px',
            cursor: 'pointer',
          }}
        >
          Start Playing
        </button>
        <button
          onClick={() => { setCreated(null); setError(''); }}
          style={{
            padding: '10px 14px',
            borderRadius: '10px',
            border: '1px solid rgba(255,255,255,0.08)',
            background: 'transparent',
            color: 'rgba(220,210,255,0.7)',
            fontSize: '12px',
            fontWeight: 700,
            cursor: 'pointer',
          }}
        >
          Pick a different difficulty or color
        </button>
      </div>
    );
  }

  const creating = inflightDifficulty !== null;
  const colorLabel = selectedColor === 'white' ? 'White' : selectedColor === 'black' ? 'Black' : 'Random Color';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
        gap: '16px',
        alignItems: 'start',
      }}>
        {/* Difficulty Selection */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <div style={{
            fontSize: '11px',
            fontWeight: 800,
            color: '#d4a0ff',
            textTransform: 'uppercase',
            letterSpacing: '1px',
          }}>
            Engine Difficulty
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '8px' }}>
            {DIFFICULTIES.map((d) => {
              const isInflight = creating && inflightDifficulty === d.value;
              const isSelected = !creating && selectedDifficulty === d.value;
              return (
                <button
                  key={d.value}
                  type="button"
                  onClick={() => handleDifficultyClick(d.value)}
                  disabled={creating}
                  style={{
                    padding: '10px 12px',
                    borderRadius: '10px',
                    border: isInflight
                      ? '1px solid rgba(180,130,255,0.6)'
                      : isSelected
                        ? '1px solid rgba(180,130,255,0.4)'
                        : '1px solid rgba(255,255,255,0.08)',
                    background: isInflight
                      ? 'rgba(180,130,255,0.22)'
                      : isSelected
                        ? 'rgba(180,130,255,0.12)'
                        : 'rgba(255,255,255,0.03)',
                    color: isInflight || isSelected ? '#e0d0ff' : 'rgba(210,200,230,0.75)',
                    cursor: creating && !isInflight ? 'wait' : 'pointer',
                    textAlign: 'left',
                    opacity: creating && !isInflight ? 0.5 : 1,
                    transition: 'all 0.15s ease',
                  }}
                >
                  <div style={{ fontSize: '13px', fontWeight: 800, display: 'flex', alignItems: 'center', gap: '6px' }}>
                    {isInflight && (
                      <span style={{
                        display: 'inline-block',
                        width: '8px',
                        height: '8px',
                        borderRadius: '50%',
                        border: '1.5px solid #d4a0ff',
                        borderTopColor: 'transparent',
                        animation: 'chess404-spin 0.7s linear infinite',
                      }} />
                    )}
                    {d.label}
                  </div>
                  <div style={{ fontSize: '11px', marginTop: '4px', opacity: 0.7 }}>{d.description}</div>
                </button>
              );
            })}
          </div>
        </div>

        {/* Color / Side Selection */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <div style={{
            fontSize: '11px',
            fontWeight: 800,
            color: '#d4a0ff',
            textTransform: 'uppercase',
            letterSpacing: '1px',
          }}>
            Your Color
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '8px' }}>
            {COLOR_CHOICES.map((c) => {
              const isSelected = selectedColor === c.value;
              return (
                <button
                  key={c.value}
                  type="button"
                  onClick={() => handleColorClick(c.value)}
                  disabled={creating}
                  data-testid={`btn-color-${c.value}`}
                  style={{
                    padding: '12px 10px',
                    borderRadius: '10px',
                    border: isSelected
                      ? c.value === 'white'
                        ? '1.5px solid rgba(255, 240, 200, 0.75)'
                        : c.value === 'black'
                          ? '1.5px solid rgba(160, 190, 255, 0.75)'
                          : '1.5px solid rgba(210, 160, 255, 0.75)'
                      : '1px solid rgba(255, 255, 255, 0.08)',
                    background: isSelected
                      ? c.value === 'white'
                        ? 'linear-gradient(180deg, rgba(255, 250, 230, 0.16) 0%, rgba(200, 180, 130, 0.10) 100%)'
                        : c.value === 'black'
                          ? 'linear-gradient(180deg, rgba(20, 28, 48, 0.75) 0%, rgba(10, 15, 28, 0.95) 100%)'
                          : 'linear-gradient(180deg, rgba(160, 110, 240, 0.22) 0%, rgba(90, 50, 160, 0.15) 100%)'
                      : 'rgba(255, 255, 255, 0.03)',
                    color: isSelected ? '#ffffff' : 'rgba(210, 200, 230, 0.75)',
                    boxShadow: isSelected
                      ? c.value === 'white'
                        ? '0 0 16px rgba(255, 240, 200, 0.18), inset 0 1px 0 rgba(255, 255, 255, 0.3)'
                        : c.value === 'black'
                          ? '0 0 16px rgba(80, 120, 200, 0.18), inset 0 1px 0 rgba(100, 150, 255, 0.2)'
                          : '0 0 16px rgba(180, 120, 255, 0.18), inset 0 1px 0 rgba(200, 150, 255, 0.25)'
                      : 'none',
                    cursor: creating ? 'wait' : 'pointer',
                    textAlign: 'center',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    gap: '4px',
                    transition: 'all 0.15s ease',
                  }}
                >
                  <div style={{
                    fontSize: '22px',
                    lineHeight: 1,
                    filter: c.value === 'white' ? 'drop-shadow(0 0 4px rgba(255,255,255,0.4))' : 'none',
                  }}>
                    {c.piece}
                  </div>
                  <div style={{ fontSize: '13px', fontWeight: 800 }}>
                    {c.label}
                  </div>
                  <div style={{ fontSize: '11px', opacity: 0.7, marginTop: '2px' }}>
                    {c.description}
                  </div>
                </button>
              );
            })}
          </div>
        </div>
      </div>

      <button
        onClick={handlePlay}
        disabled={creating}
        style={{
          width: '100%',
          padding: '12px 16px',
          borderRadius: '10px',
          border: '1px solid rgba(180,130,255,0.36)',
          background: creating
            ? 'rgba(130,80,210,0.5)'
            : 'linear-gradient(180deg, rgba(130,80,210,0.95) 0%, rgba(70,40,130,0.98) 100%)',
          color: '#f7fbff',
          fontWeight: 800,
          fontSize: '13px',
          cursor: creating ? 'default' : 'pointer',
          opacity: creating ? 0.7 : 1,
        }}
      >
        {creating
          ? 'Creating match...'
          : `Play as ${colorLabel} vs ${DIFFICULTIES.find((d) => d.value === selectedDifficulty)?.label ?? 'Medium'} Computer`}
      </button>

      {error && (
        <div style={{
          padding: '10px 12px',
          borderRadius: '8px',
          background: 'rgba(255,80,80,0.1)',
          border: '1px solid rgba(255,80,80,0.2)',
          color: '#ff8888',
          fontSize: '12px',
        }}>
          {error}
          <div style={{ marginTop: '6px', opacity: 0.7, fontSize: '11px' }}>
            Click Play to try again.
          </div>
        </div>
      )}

      {creating && (
        <div style={{ fontSize: '11px', color: 'rgba(220,210,255,0.6)', textAlign: 'center' }}>
          Creating match as {inflightColor ?? 'selected color'} against the {DIFFICULTIES.find((d) => d.value === inflightDifficulty)?.label.toLowerCase()} engine…
        </div>
      )}
    </div>
  );
}
