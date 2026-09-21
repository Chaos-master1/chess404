'use client';

import type { CardPendingState, GameCard, PendingCardState } from '@chess404/contracts';

/**
 * Rebuilds the local pending-card UI state from the authoritative snapshot's
 * `pendingCard` field. Every snapshot applier must run this: after a server
 * restart, a reconnect, or a `play_card` intent response the client's local
 * pending state is otherwise empty, leaving the server's armed target
 * selection undiscoverable (the user can neither complete nor cancel it).
 *
 * Returns null when the server has no pending card, when it belongs to the
 * Joker's inline picker, or when the owning hand no longer holds the card.
 */
export function buildPendingCardFromSnapshot(
  pending: PendingCardState | null | undefined,
  whiteCards: GameCard[],
  blackCards: GameCard[],
): CardPendingState {
  if (!pending || pending.mechanic === 'joker') return null;
  const ownerCards = pending.ownerColor === 'white' ? whiteCards : blackCards;
  const card = ownerCards.find(item => item.id === pending.cardId);
  if (!card) return null;
  const mechanic = pending.mechanic;
  const target = pending.target ?? undefined;
  return {
    card,
    playerColor: pending.ownerColor,
    mechanic,
    step: pending.target ? 2 : 1,
    data: {
      sq: target,
      from: mechanic === 'teleport' || mechanic === 'jump' || mechanic === 'clone' ? target : undefined,
      sq1: ['swapme', 'swapus', 'swaphim', 'halffuse', 'fullfusion'].includes(mechanic) ? target : undefined,
      hostSq: mechanic === 'parasite' ? target : undefined,
      type1: mechanic === 'halffuse' || mechanic === 'fullfusion' ? pending.options?.[0] : undefined,
      selected: mechanic === 'smallsacrifice' || mechanic === 'bigsacrifice'
        ? (pending.options ?? []).map(v => { const [r, c] = v.split(',').map(Number); return { row: r, col: c }; }).filter(sq => Number.isInteger(sq.row) && Number.isInteger(sq.col))
        : undefined,
      options: pending.options ?? undefined,
    },
  };
}
