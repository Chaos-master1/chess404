import React from 'react';

export interface PlayerBarProps {
  seat: 'white' | 'black';
  playerName: string;
  rating: string | number;
  /** True when the seat has no real rating (waiting/guest): render nothing. */
  ratingHidden?: boolean;
  timeMs: number;
  isClockActive: boolean;
  seatBadge?: string;
  /** Match time control label (e.g. "10+0"); empty/undefined hides the chip. */
  timeControl?: string;
}

export function formatClock(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export function PlayerBar({
  seat,
  playerName,
  rating,
  ratingHidden = false,
  timeMs,
  isClockActive,
  seatBadge,
  timeControl
}: PlayerBarProps) {
  const isWhite = seat === 'white';
  const timeUrgent = timeMs <= 30000;
  
  return (
    <div className={`card-surface player-bar player-bar--${seat}`}>
      <div className="player-bar__avatar">
        {/* Placeholder avatar */}
        <span>{isWhite ? '🕵️' : '👤'}</span>
      </div>
      
      <div className="player-bar__info">
        <div className="player-bar__name-row">
          <span className="player-bar__name">{playerName}</span>
          {seatBadge && <span className={`badge ${seatBadge === 'You' ? 'badge--success' : ''}`}>{seatBadge}</span>}
          {timeControl && <span className="player-bar__timecontrol mono" title="Time control">⏱ {timeControl}</span>}
        </div>
        <div className="player-bar__stats">
          {!ratingHidden && <span className="player-bar__rating">♟ {rating}</span>}
          <span className={`player-bar__clock mono ${timeUrgent ? 'player-bar__clock--urgent' : ''} ${isClockActive ? 'player-bar__clock--active' : ''}`}>
            ⏱ {formatClock(timeMs)}
          </span>
        </div>
      </div>
    </div>
  );
}
