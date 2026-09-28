import { DEFAULT_MATCH_MODE_ID, OFFICIAL_MATCH_MODES, type MatchFinishReason, type MatchModeId } from '@chess404/contracts';

export function formatDateTime(value?: string | null): string {
  if (!value) {
    return 'Unknown';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function normalizeModeId(value?: string | null): MatchModeId {
  return value === 'hidden_cards' ? 'hidden_cards' : DEFAULT_MATCH_MODE_ID;
}

export function formatModeLabel(modeId?: string | null): string {
  const normalized = normalizeModeId(modeId);
  return OFFICIAL_MATCH_MODES.find((mode) => mode.id === normalized)?.label ?? 'Open Cards';
}

export function formatQueueLabel(queue?: string | null): string {
  if (queue === 'rated') {
    return 'Rated';
  }
  if (queue === 'casual') {
    return 'Casual';
  }
  if (queue === 'direct') {
    return 'Private';
  }
  return 'Unranked';
}

export function formatFinishReasonLabel(reason?: MatchFinishReason | null): string | null {
  switch (reason) {
    case 'checkmate':
      return 'Checkmate';
    case 'stalemate':
      return 'Stalemate';
    case 'insufficient_material':
      return 'Insufficient material';
    case 'threefold_repetition':
      return 'Threefold repetition';
    case 'fifty_move_rule':
      return '50-move rule';
    case 'timeout':
      return 'Timeout';
    case 'abandon':
      return 'Abandonment';
    case 'resign':
      return 'Resignation';
    case 'abort':
      return 'Early abort';
    case 'draw_agreement':
      return 'Draw agreement';
    default:
      return null;
  }
}

export function formatPlayerLabel(options: {
  name?: string | null;
  handle?: string | null;
  fallback: string;
}): string {
  // Lichess-style identity: only account handles are shown. Players without
  // an account are Anonymous — never a generated guest name. (Old archive
  // rows may still carry a pre-claim guest name; it is deliberately hidden.)
  const handle = options.handle?.trim();
  if (handle) {
    return `@${handle}`;
  }
  return options.fallback;
}

export function formatMatchPlayers(options: {
  whiteName?: string | null;
  whiteHandle?: string | null;
  blackName?: string | null;
  blackHandle?: string | null;
}): string {
  const white = formatPlayerLabel({
    name: options.whiteName,
    handle: options.whiteHandle,
    fallback: 'Anonymous',
  });
  const black = formatPlayerLabel({
    name: options.blackName,
    handle: options.blackHandle,
    fallback: 'Anonymous',
  });
  return `${white} vs ${black}`;
}

export function formatMatchResult(options: {
  status?: string | null;
  winner?: string | null;
  finishReason?: MatchFinishReason | null;
}): string {
  const finish = formatFinishReasonLabel(options.finishReason);
  if (options.status === 'active') {
    return 'Live now';
  }
  if (options.winner === 'draw') {
    return finish ? `Draw by ${finish.toLowerCase()}` : 'Draw';
  }
  if (options.winner === 'aborted') {
    return finish ? `Aborted by ${finish.toLowerCase()}` : 'Aborted';
  }
  if (options.winner === 'white' || options.winner === 'black') {
    return finish ? `${options.winner === 'white' ? 'White' : 'Black'} won by ${finish.toLowerCase()}` : `${options.winner === 'white' ? 'White' : 'Black'} won`;
  }
  if (finish) {
    return finish;
  }
  return options.status ?? 'Finished';
}

export function formatMatchFormat(queue?: string | null, modeId?: string | null): string {
  return `${formatQueueLabel(queue)} - ${formatModeLabel(modeId)}`;
}

export function formatMoveCountLabel(moveCount?: number | null): string {
  const safeCount = typeof moveCount === 'number' && moveCount > 0 ? moveCount : 0;
  return `${safeCount} ${safeCount === 1 ? 'move' : 'moves'}`;
}

export function formatMatchSummary(options: {
  status?: string | null;
  winner?: string | null;
  finishReason?: MatchFinishReason | null;
  queue?: string | null;
  modeId?: string | null;
  moveCount?: number | null;
  updatedAt?: string | null;
  includeUpdatedAt?: boolean;
}): string {
  const parts = [
    formatMatchResult({
      status: options.status,
      winner: options.winner,
      finishReason: options.finishReason,
    }),
    formatMatchFormat(options.queue, options.modeId),
    formatMoveCountLabel(options.moveCount),
  ];
  if (options.includeUpdatedAt && options.updatedAt) {
    parts.push(`Updated ${formatDateTime(options.updatedAt)}`);
  }
  return parts.join(' - ');
}

export function formatLastSeenLabel(value?: string | null): string {
  return `Seen ${formatDateTime(value)}`;
}

export function formatRatingDelta(delta: number): string {
  return delta > 0 ? `+${delta}` : `${delta}`;
}
