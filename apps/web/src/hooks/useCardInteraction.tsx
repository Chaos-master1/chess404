'use client';

import React from 'react';
import type {
  Board,
  PieceType,
  PieceColor,
  Piece,
  Sq,
  GameCard,
  CardAnimType,
  CardMechanic,
  CardPendingState,
  DoubleMove,
  LavaSquare,
  BombPiece,
  FogZone,
  FortressZone,
  Rarity,
  Snapshot,
} from '../types';
import type { MatchSnapshotMessage, PlayerIntent } from '@chess404/contracts';
import { applyIntent } from '../lib/match-service';
import {
  findKing,
  legalMoves,
  toFEN,
} from '../chessEngine';
import { CARD_POOL, incrementCardSeq } from '../cardPool';
import {
  RARITY_STYLE,
  OPP,
  FILES,
  RANKS,
  PIECE_VALUE,
  TARGETING_CARDS,
  CARD_TARGET_MESSAGES,
} from '../constants';

export const AUTHORITATIVE_JOKER_MECHANICS = new Set<CardMechanic>([
  'freeze', 'shield', 'sniper', 'badsniper', 'promote', 'demote', 'promotehim', 'demotehim',
  'teleport', 'jump', 'doublemove_diff', 'doublemove_same', 'swapme', 'swapus', 'swaphim',
  'borrow', 'mindcontrol', 'parasite', 'clone', 'fakepiece', 'lavaground', 'blackhole',
  'fortress',
  'fog_village', 'invisible', 'unabomber', 'halffuse', 'fullfusion', 'reverse', 'undo',
  'mirror', 'smallsacrifice', 'bigsacrifice', 'gambler', 'radar', 'cheater'
]);

export interface UseCardInteractionProps {
  board: Board;
  setBoard: React.Dispatch<React.SetStateAction<Board>>;
  turn: PieceColor;
  setTurn: React.Dispatch<React.SetStateAction<PieceColor>>;
  moved: Set<string>;
  setMoved: React.Dispatch<React.SetStateAction<Set<string>>>;
  lm: { from: Sq; to: Sq } | null;
  setLm: React.Dispatch<React.SetStateAction<{ from: Sq; to: Sq } | null>>;
  fmn: number;
  fmnRef: React.MutableRefObject<number>;
  boardRef: React.MutableRefObject<Board>;
  turnRef: React.MutableRefObject<PieceColor>;
  whiteHand: GameCard[];
  setWhiteHand: React.Dispatch<React.SetStateAction<GameCard[]>>;
  blackHand: GameCard[];
  setBlackHand: React.Dispatch<React.SetStateAction<GameCard[]>>;
  selectedCard: GameCard | null;
  setSelectedCard: React.Dispatch<React.SetStateAction<GameCard | null>>;
  cardPending: CardPendingState;
  setCardPending: React.Dispatch<React.SetStateAction<CardPendingState>>;
  cardMsg: string;
  setCardMsg: React.Dispatch<React.SetStateAction<string>>;
  promoPicker: { sq: Sq; options: PieceType[]; mechanic: CardMechanic } | null;
  setPromoPicker: React.Dispatch<React.SetStateAction<{ sq: Sq; options: PieceType[]; mechanic: CardMechanic } | null>>;
  cardPromo: { sq: Sq; color: PieceColor } | null;
  setCardPromo: React.Dispatch<React.SetStateAction<{ sq: Sq; color: PieceColor } | null>>;
  cardUsedBy: { white: boolean; black: boolean };
  setCardUsedBy: React.Dispatch<React.SetStateAction<{ white: boolean; black: boolean }>>;
  jokerPicker: { card: GameCard; playerColor: PieceColor; filterRarity: Rarity | 'all'; transforming: boolean } | null;
  setJokerPicker: React.Dispatch<React.SetStateAction<{ card: GameCard; playerColor: PieceColor; filterRarity: Rarity | 'all'; transforming: boolean } | null>>;
  doubleMove: DoubleMove | null;
  setDoubleMove: React.Dispatch<React.SetStateAction<DoubleMove | null>>;
  doubleMoveRef: React.MutableRefObject<DoubleMove | null>;
  pendingCardUseRef: React.MutableRefObject<Set<string>>;
  cardUsedByRef: React.MutableRefObject<{ white: boolean; black: boolean }>;
  ghostRef: React.MutableRefObject<{ piece: Piece; row: number; col: number; ownerColor: PieceColor; roundsLeft: number } | null>;
  ghostPiece: { piece: Piece; row: number; col: number; ownerColor: PieceColor; roundsLeft: number } | null;
  setGhostPiece: React.Dispatch<React.SetStateAction<{ piece: Piece; row: number; col: number; ownerColor: PieceColor; roundsLeft: number } | null>>;
  lavaSquares: LavaSquare[];
  setLavaSquares: React.Dispatch<React.SetStateAction<LavaSquare[]>>;
  setLavaExploding: React.Dispatch<React.SetStateAction<Sq[]>>;
  bombPieces: BombPiece[];
  setBombPieces: React.Dispatch<React.SetStateAction<BombPiece[]>>;
  setBombExploding: React.Dispatch<React.SetStateAction<Sq[]>>;
  setSwapAnim: React.Dispatch<React.SetStateAction<{ sq1: Sq; sq2: Sq; color1: string; color2: string } | null>>;
  fogZones: FogZone[];
  setFogZones: React.Dispatch<React.SetStateAction<FogZone[]>>;
  fortressZones: FortressZone[];
  setFortressZones: React.Dispatch<React.SetStateAction<FortressZone[]>>;
  authoritativeMatchIdRef: React.MutableRefObject<string | null>;
  authoritativeActorForColor: (color: PieceColor) => { playerId: string; playerSecret?: string; playerClaimToken?: string };
  applyAuthoritativeSnapshot: (snapshot: MatchSnapshotMessage) => void;
  fireCardAnim: (type: CardAnimType, label?: string) => void;
  playMoveSound: (type?: 'move' | 'capture' | 'check' | 'castle' | 'card' | 'victory' | 'defeat' | 'lava' | 'bomb' | 'shield') => void;
  playCardSound: (mechanic?: CardMechanic) => void;
  analyse: (fen: string, turn: PieceColor) => void;
  isAttackedWithFusion: (b: Board, row: number, col: number, byColor: PieceColor) => boolean;
  checkEndGame: (nb: Board, next: PieceColor, newMv: Set<string>, newLm: { from: Sq; to: Sq } | null, newHmc: number, newPh: string[], posKey: string, fen: string, t: PieceColor) => void;
  finishCardUse: (card: GameCard, playerColor: PieceColor) => void;
  removeCardFromHand: (card: GameCard, playerColor: PieceColor) => void;
  radarActive: boolean;
  setRadarActive: React.Dispatch<React.SetStateAction<boolean>>;
  finalPositionRef: React.MutableRefObject<{ fen: string; turn: PieceColor } | null>;
  setOver: React.Dispatch<React.SetStateAction<boolean>>;
  setWinner: React.Dispatch<React.SetStateAction<PieceColor | 'draw' | 'aborted' | null>>;
  setMovHist: React.Dispatch<React.SetStateAction<any[]>>;
  setPosHist: React.Dispatch<React.SetStateAction<string[]>>;
  setSnapshots: React.Dispatch<React.SetStateAction<Snapshot[]>>;
  triggerSniperAnim: (sq: Sq, type: PieceType, color: PieceColor, mechanic: 'sniper' | 'badsniper') => void;
  triggerTransformAnim: (sq: Sq, dir: 'up' | 'down', from: PieceType, to: PieceType, color: PieceColor) => void;
  triggerFuseAnim: (anim: { sq1: Sq; sq2: Sq; type1: PieceType; type2: PieceType; color: PieceColor }) => void;
  triggerSwapAnim: (sq1: Sq, sq2: Sq, color1?: string, color2?: string) => void;
  triggerTeleportAnim: (fromSq: Sq, toSq: Sq, type: PieceType, color: PieceColor) => void;
  triggerJumpAnim: (fromSq: Sq, toSq: Sq, type: PieceType, color: PieceColor, captured: boolean) => void;
  triggerMindControlAnim: (targetSq: Sq, playerColor: PieceColor, pieceType: PieceType) => void;
  triggerSacrificeAnim: (squares: Sq[]) => void;
  triggerCloneAnim?: (fromSq: Sq, toSq: Sq, type: PieceType, color: PieceColor) => void;
  triggerBlackHoleAnim?: (center: Sq) => void;
  over: boolean;
  hostedRuntime: boolean | null;
  viewerSeatRef: React.MutableRefObject<PieceColor | null>;
}

export function useCardInteraction(props: UseCardInteractionProps) {
  const {
    board, setBoard, turn, setTurn, moved, setMoved, lm, setLm, fmn, fmnRef, boardRef, turnRef,
    whiteHand, setWhiteHand, blackHand, setBlackHand, selectedCard, setSelectedCard,
    cardPending, setCardPending, cardMsg, setCardMsg, promoPicker, setPromoPicker,
    cardPromo, setCardPromo, cardUsedBy, setCardUsedBy, jokerPicker, setJokerPicker,
    doubleMove, setDoubleMove, doubleMoveRef, pendingCardUseRef, cardUsedByRef,
    ghostRef, ghostPiece, setGhostPiece, lavaSquares, setLavaSquares, setLavaExploding,
    bombPieces, setBombPieces, setBombExploding, setSwapAnim, fogZones, setFogZones,
    fortressZones, setFortressZones, authoritativeMatchIdRef, authoritativeActorForColor,
    applyAuthoritativeSnapshot, fireCardAnim, playMoveSound, playCardSound, analyse,
    isAttackedWithFusion, checkEndGame, finishCardUse, removeCardFromHand, radarActive,
    setRadarActive, finalPositionRef, setOver, setWinner, setMovHist, setPosHist, setSnapshots,
    triggerSniperAnim, triggerTransformAnim, triggerFuseAnim,
    triggerSwapAnim, triggerTeleportAnim, triggerJumpAnim, triggerMindControlAnim, triggerSacrificeAnim,
    triggerCloneAnim, triggerBlackHoleAnim,
    over, hostedRuntime, viewerSeatRef
  } = props;

  const jokerPickerRef = React.useRef<typeof jokerPicker>(null);
  React.useEffect(() => { jokerPickerRef.current = jokerPicker; }, [jokerPicker]);

  const cancelCard = React.useCallback(() => {
    if (cardPending) pendingCardUseRef.current.delete(cardPending.card.id);
    const jp = jokerPickerRef.current;
    if (jp) pendingCardUseRef.current.delete(jp.card.id);
    // The server keeps PendingCard armed until it is told otherwise -- a purely
    // local dismiss used to leave it armed forever, so every later play_card
    // was rejected with "resolve the pending card target first" for the rest
    // of the match. Tell the authoritative server the pending card was
    // abandoned; the hand is untouched because a pending card is only removed
    // from the hand when its target RESOLVES.
    if (hostedRuntime && authoritativeMatchIdRef.current && cardPending) {
      const actor = authoritativeActorForColor(cardPending.playerColor);
      if (actor.playerId && (actor.playerSecret || actor.playerClaimToken)) {
        // Omit<Union> collapses union members, so Extract the cancel_card
        // member first (the same pattern every other intent call site uses).
        const cancelIntent: Omit<Extract<PlayerIntent, { type: 'cancel_card' }>, 'matchId'> = {
          type: 'cancel_card',
          ...actor,
          cardId: cardPending.card.id,
        };
        void applyIntent(authoritativeMatchIdRef.current, cancelIntent).then(snapshot => {
          applyAuthoritativeSnapshot(snapshot);
        }).catch(() => {
          // The pending state is already cleared locally; a failed sync leaves
          // the server pending armed, but the next successful snapshot apply
          // restores the client view from authoritative state.
        });
      }
    }
    setJokerPicker(null);
    setCardPending(null);
    setCardMsg('');
    setPromoPicker(null);
    setCardPromo(null);
    setSelectedCard(null);
  }, [cardPending, pendingCardUseRef, setCardMsg, setCardPending, setCardPromo, setJokerPicker, setPromoPicker, setSelectedCard, hostedRuntime, authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot]);

  const getSafeTransforms = React.useCallback((
    b: Board,
    row: number,
    col: number,
    transforms: PieceType[],
    playerColor: PieceColor,
  ): PieceType[] => {
    const opp = OPP[playerColor];
    const piece = b[row][col]!;
    return transforms.filter(t => {
      const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
      nb[row][col] = { ...piece, type: t };
      const kp  = findKing(nb, playerColor);
      const okp = findKing(nb, opp);
      return (
        !(kp  && isAttackedWithFusion(nb, kp.row,  kp.col,  opp))        &&
        !(okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor))
      );
    });
  }, [isAttackedWithFusion]);

  const getFusedMoves = React.useCallback((
    b: Board,
    row: number,
    col: number,
    type1: PieceType,
    type2: PieceType,
  ): Sq[] => {
    const piece = b[row][col]!;
    const boardAs1: Board = b.map(r => r.map(p => p ? { ...p } : null));
    boardAs1[row][col] = { ...piece, type: type1, fusedWith: undefined };
    const boardAs2: Board = b.map(r => r.map(p => p ? { ...p } : null));
    boardAs2[row][col] = { ...piece, type: type2, fusedWith: undefined };
    const moves1 = legalMoves(boardAs1, row, col, lm, moved);
    const moves2 = legalMoves(boardAs2, row, col, lm, moved);
    const seen = new Set<string>();
    return [...moves1, ...moves2].filter(sq => {
      const key = `${sq.row},${sq.col}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  }, [lm, moved]);

  const checkFusionRedundancy = React.useCallback((
    typeA: PieceType,
    typeB: PieceType,
  ): string | null => {
    if (typeA === typeB) return `⚗️ Can't fuse two ${typeA}s — same piece type adds nothing!`;
    if ((typeA === 'queen' && typeB === 'rook') || (typeA === 'rook' && typeB === 'queen'))
      return '⚗️ Queen already moves like a rook — nothing to gain!';
    if ((typeA === 'queen' && typeB === 'bishop') || (typeA === 'bishop' && typeB === 'queen'))
      return '⚗️ Queen already moves like a bishop — nothing to gain!';
    if ((typeA === 'queen' && typeB === 'pawn') || (typeA === 'pawn' && typeB === 'queen'))
      return '⚗️ Queen already outclasses pawn movement — nothing to gain!';
    if (typeA === 'bishop' && typeB === 'bishop')
      return '⚗️ Bishops are locked to their square color — fusing them adds no new movement!';
    return null;
  }, []);

  const activateDoubleMove = React.useCallback((type: 'diff' | 'same', card: GameCard, playerColor: PieceColor) => {
    const newDm: DoubleMove = { type, movesLeft: 2, trackedSq: null };
    doubleMoveRef.current = newDm;
    setDoubleMove(newDm);
    setCardMsg(
      type === 'diff'
        ? '👥 Twin active! Make your first move with any piece, then move a DIFFERENT piece.'
        : '🏃 Solo active! Make your first move, then move the SAME piece again.'
    );
    setTimeout(() => setCardMsg(''), 4000);
    finishCardUse(card, playerColor);
  }, [doubleMoveRef, finishCardUse, setCardMsg, setDoubleMove]);

  const openJokerPicker = React.useCallback((card: GameCard, playerColor: PieceColor) => {
    setJokerPicker({ card, playerColor, filterRarity: 'all', transforming: false });
    setSelectedCard(null);
    pendingCardUseRef.current.add(card.id);
  }, [pendingCardUseRef, setJokerPicker, setSelectedCard]);

  const applyJokerTransform = React.useCallback((jokerCard: GameCard, playerColor: PieceColor, chosenTemplate: Omit<GameCard, 'id'>) => {
    setJokerPicker(prev => prev ? { ...prev, transforming: true } : null);
    setTimeout(() => {
      if (authoritativeMatchIdRef.current) {
        const transformIntent: Omit<Extract<PlayerIntent, { type: 'select_target' }>, 'matchId'> = {
          type: 'select_target',
          ...authoritativeActorForColor(playerColor),
          selectionId: chosenTemplate.mechanic,
        };
        void applyIntent(authoritativeMatchIdRef.current, transformIntent).then(snapshot => {
          applyAuthoritativeSnapshot(snapshot);
          cardUsedByRef.current = { ...cardUsedByRef.current, [playerColor]: false };
          setCardUsedBy(prev => ({ ...prev, [playerColor]: false }));
          pendingCardUseRef.current.delete(jokerCard.id);
          setJokerPicker(null);
          setCardMsg(`🃏 Joker transformed into ${chosenTemplate.name} ${chosenTemplate.icon}!`);
          setTimeout(() => setCardMsg(''), 3000);
        }).catch(err => {
          pendingCardUseRef.current.delete(jokerCard.id);
          setJokerPicker(null);
          const message = err instanceof Error ? err.message : 'Joker transform failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      const style = RARITY_STYLE[chosenTemplate.rarity];
      const newCard: GameCard = {
        ...chosenTemplate,
        id: `joker_transformed_${incrementCardSeq()}_${Date.now()}`,
        color: style.color,
        accent: style.accent,
      };
      if (playerColor === 'white') {
        setWhiteHand(h => h.map(c => c.id === jokerCard.id ? newCard : c));
      } else {
        setBlackHand(h => h.map(c => c.id === jokerCard.id ? newCard : c));
      }
      cardUsedByRef.current = { ...cardUsedByRef.current, [playerColor]: false };
      setCardUsedBy(prev => ({ ...prev, [playerColor]: false }));
      pendingCardUseRef.current.delete(jokerCard.id);
      setJokerPicker(null);
      setCardMsg(`🃏 Joker transformed into ${chosenTemplate.name} ${chosenTemplate.icon}!`);
      setTimeout(() => setCardMsg(''), 3000);
    }, 800);
  }, [applyAuthoritativeSnapshot, authoritativeActorForColor, authoritativeMatchIdRef, cardUsedByRef, pendingCardUseRef, setBlackHand, setCardMsg, setCardUsedBy, setJokerPicker, setWhiteHand]);

  const handlePromoPick = React.useCallback((type: PieceType) => {
    if (!cardPending || !promoPicker) return;
    const { card, playerColor, mechanic } = cardPending;
    const sq = promoPicker.sq;
    const oldType = board[sq.row][sq.col]?.type ?? 'pawn';
    const pieceColor = board[sq.row][sq.col]?.color ?? playerColor;
    if (authoritativeMatchIdRef.current && (mechanic === 'promote' || mechanic === 'demote' || mechanic === 'promotehim' || mechanic === 'demotehim')) {
      const targetIntent: Omit<Extract<PlayerIntent, { type: 'select_target' }>, 'matchId'> = {
        type: 'select_target',
        ...authoritativeActorForColor(playerColor),
        selectionId: type,
      };

      void applyIntent(authoritativeMatchIdRef.current, targetIntent).then(snapshot => {
        triggerTransformAnim(sq, (mechanic === 'promote' || mechanic === 'promotehim') ? 'up' : 'down', oldType, type, pieceColor);
        applyAuthoritativeSnapshot(snapshot);
        setCardMsg(`⬆️ ${FILES[sq.col]}${RANKS[sq.row]} ${(mechanic === 'promote' || mechanic === 'promotehim') ? 'promoted' : 'demoted'} to ${type}!`);
        setTimeout(() => setCardMsg(''), 2000);
        finishCardUse(card, playerColor);
      }).catch(err => {
        const message = err instanceof Error ? err.message : 'Transform selection failed';
        setCardMsg(message);
        setTimeout(() => setCardMsg(''), 2000);
        finishCardUse(card, playerColor);
      });
      return;
    }
    setPromoPicker(null);
    const isPromotion = mechanic === 'promote' || mechanic === 'promotehim';
    triggerTransformAnim(sq, isPromotion ? 'up' : 'down', oldType, type, pieceColor);
    setTimeout(() => {
      setBoard(b => {
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        nb[sq.row][sq.col] = { ...nb[sq.row][sq.col]!, type };
        return nb;
      });
    }, 850);
    const verb = isPromotion ? 'promoted' : 'demoted';
    setCardMsg(`${isPromotion ? '⬆️' : '⬇️'} ${FILES[sq.col]}${RANKS[sq.row]} ${verb} to ${type}!`);
    setTimeout(() => setCardMsg(''), 2000);
    finishCardUse(card, playerColor);
  }, [cardPending, promoPicker, board, finishCardUse, triggerTransformAnim, applyAuthoritativeSnapshot, authoritativeActorForColor, authoritativeMatchIdRef, setBoard, setCardMsg, setPromoPicker]);

  const canUseCard = React.useCallback((card: GameCard, playerColor: PieceColor): boolean => {
    if (over) return false;
    if (hostedRuntime) {
      if (viewerSeatRef.current !== playerColor) return false;
      const actor = authoritativeActorForColor(playerColor);
      if (!actor.playerId || (!actor.playerSecret && !actor.playerClaimToken)) return false;
    }
    if (turn !== playerColor) return false;
    return !cardUsedByRef.current[playerColor];
  }, [over, turn, hostedRuntime, authoritativeActorForColor, cardUsedByRef, viewerSeatRef]);

  const handleCardClick = React.useCallback((row: number, col: number) => {
    if (!cardPending) return;
    const { card, playerColor, mechanic, step, data } = cardPending;
    const b = board;
    const piece = b[row][col];
    const opp   = OPP[playerColor];

    // Helper for sending authoritative target intents
    const sendAuthoritativeTarget = (target: Sq, extra?: Partial<Extract<PlayerIntent, { type: 'select_target' }>>) => {
      if (!authoritativeMatchIdRef.current) return Promise.resolve(null);
      const targetIntent: Omit<Extract<PlayerIntent, { type: 'select_target' }>, 'matchId'> = {
        type: 'select_target',
        ...authoritativeActorForColor(playerColor),
        target,
        ...extra,
      };
      return applyIntent(authoritativeMatchIdRef.current, targetIntent).then(snapshot => {
        applyAuthoritativeSnapshot(snapshot);
        if (!snapshot.match?.pendingCard) {
          setSelectedCard(null);
        }
        return snapshot;
      });
    };

    // ─── SWAP US (swap 1 own piece with 1 enemy piece) ───
    if (mechanic === 'swapus') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('↔️ Click YOUR piece to swap with enemy (not king)');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        setCardMsg('↔️ Now click an ENEMY piece to swap with (not king)');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq1: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const sq1 = (data.sq1 as Sq) || (data.sq as Sq) || (cardPending.data?.sq1 as Sq) || (cardPending.data?.sq as Sq);
        if (!sq1) {
          setCardMsg('↔️ Click YOUR piece first');
          return;
        }
        if (!piece || piece.color !== opp || piece.type === 'king') {
          setCardMsg('↔️ Must pick an ENEMY piece (not king)!');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const p1 = nb[sq1.row]?.[sq1.col];
        const p2 = nb[row]?.[col];
        if (!p1 || !p2) {
          setCardMsg('↔️ Pieces for swap not found');
          return;
        }
        nb[sq1.row][sq1.col] = p2;
        nb[row][col] = p1;
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('↔️ That swap would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('↔️ That swap would put enemy king in check — not allowed!');
          return;
        }

        triggerSwapAnim(sq1, { row, col }, '#4ade80', '#f87171');
        fireCardAnim('swap', `Swapped ${p1.type} ↔ ${p2.type}`);
        playMoveSound('move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`↔️ Swapped ${p1.type} with enemy ${p2.type}!`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Swap failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`↔️ Swapped ${p1.type} with enemy ${p2.type}!`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── SWAP ME (swap 2 of your own pieces) ───
    if (mechanic === 'swapme') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🔄 Click the FIRST of your pieces to swap (not king)');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        setCardMsg('🔄 Now click the SECOND of your pieces to swap with');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq1: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const sq1 = (data.sq1 as Sq) || (data.sq as Sq) || (cardPending.data?.sq1 as Sq) || (cardPending.data?.sq as Sq);
        if (!sq1) {
          setCardMsg('🔄 Click YOUR first piece first');
          return;
        }
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🔄 Must pick YOUR piece (not king)!');
          return;
        }
        if (row === sq1.row && col === sq1.col) {
          setCardMsg('🔄 Pick a different piece!');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const p1 = nb[sq1.row]?.[sq1.col];
        const p2 = nb[row]?.[col];
        if (!p1 || !p2) {
          setCardMsg('🔄 Pieces for swap not found');
          return;
        }
        nb[sq1.row][sq1.col] = p2;
        nb[row][col] = p1;
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🔄 That swap would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('🔄 That swap would put enemy king in check!');
          return;
        }

        triggerSwapAnim(sq1, { row, col }, '#4ade80', '#4ade80');
        fireCardAnim('swap', `Swapped ${p1.type} ↔ ${p2.type}`);
        playMoveSound('move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🔄 Swapped ${p1.type} and ${p2.type}!`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Swap failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🔄 Swapped ${p1.type} and ${p2.type}!`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── SWAP HIM (swap 2 enemy pieces) ───
    if (mechanic === 'swaphim') {
      if (step === 1) {
        if (!piece || piece.color !== opp || piece.type === 'king') {
          setCardMsg('🔁 Click FIRST enemy piece to swap (not king)');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        setCardMsg('🔁 Now click the SECOND enemy piece to swap with (not king)');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq1: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const sq1 = (data.sq1 as Sq) || (data.sq as Sq) || (cardPending.data?.sq1 as Sq) || (cardPending.data?.sq as Sq);
        if (!sq1) {
          setCardMsg('🔁 Click FIRST enemy piece first');
          return;
        }
        if (!piece || piece.color !== opp || piece.type === 'king') {
          setCardMsg('🔁 Must pick an ENEMY piece (not king)!');
          return;
        }
        if (row === sq1.row && col === sq1.col) {
          setCardMsg('🔁 Pick a different piece!');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot swap a frozen piece!');
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const p1 = nb[sq1.row]?.[sq1.col];
        const p2 = nb[row]?.[col];
        if (!p1 || !p2) {
          setCardMsg('🔁 Pieces for swap not found');
          return;
        }
        nb[sq1.row][sq1.col] = p2;
        nb[row][col] = p1;
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🔁 That swap would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('🔁 That swap would put enemy king in check — not allowed!');
          return;
        }

        triggerSwapAnim(sq1, { row, col }, '#f87171', '#f87171');
        fireCardAnim('swap', `Swapped enemy ${p1.type} ↔ ${p2.type}`);
        playMoveSound('move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🔁 Swapped enemy ${p1.type} and ${p2.type}!`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Swap failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🔁 Swapped enemy ${p1.type} and ${p2.type}!`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── TELEPORT ───
    if (mechanic === 'teleport') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🌀 Click YOUR piece to teleport (not king)');
          return;
        }
        setCardMsg('🌀 Now click an empty destination square');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, from: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const from = (data.from as Sq) || (cardPending.data.from as Sq);
        if (!from) { setCardMsg('🌀 Click YOUR piece first'); return; }
        if (piece) { setCardMsg('🌀 Destination square must be empty!'); return; }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const src = nb[from.row][from.col]!;
        nb[row][col] = src;
        nb[from.row][from.col] = null;
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🌀 Teleport would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('🌀 Cannot teleport there — would put enemy king in check!');
          return;
        }

        triggerTeleportAnim(from, { row, col }, src.type, playerColor);
        fireCardAnim('teleport', `Teleported to ${FILES[col]}${RANKS[row]}`);
        playMoveSound('move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🌀 Teleported to ${FILES[col]}${RANKS[row]}`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Teleport failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🌀 Teleported to ${FILES[col]}${RANKS[row]}`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── JUMP ───
    if (mechanic === 'jump') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🦘 Click YOUR piece to jump (not king)');
          return;
        }
        setCardMsg('🦘 Now click landing square');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, from: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const from = (data.from as Sq) || (cardPending.data.from as Sq);
        if (!from) { setCardMsg('🦘 Click YOUR piece first'); return; }
        if (piece && piece.color === playerColor) { setCardMsg('🦘 Cannot land on your own piece!'); return; }
        if (piece && piece.type === 'king') { setCardMsg('🦘 Cannot capture king!'); return; }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const src = nb[from.row][from.col]!;
        nb[row][col] = src;
        nb[from.row][from.col] = null;
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🦘 Jump would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('🦘 Cannot jump there — would put enemy king in check!');
          return;
        }

        triggerJumpAnim(from, { row, col }, src.type, playerColor, Boolean(piece));
        fireCardAnim('teleport', `Jumped to ${FILES[col]}${RANKS[row]}`);
        playMoveSound(piece ? 'capture' : 'move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🦘 Jumped to ${FILES[col]}${RANKS[row]}`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Jump failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🦘 Jumped to ${FILES[col]}${RANKS[row]}`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── CLONE ───
    if (mechanic === 'clone') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🧬 Click YOUR piece to clone (not king)');
          return;
        }
        setCardMsg('🧬 Now click an adjacent empty square to place the clone');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, from: { row, col } } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const from = (data.from as Sq) || (cardPending.data.from as Sq);
        if (!from) { setCardMsg('🧬 Click YOUR piece first'); return; }
        if (piece) { setCardMsg('🧬 Target square must be EMPTY!'); return; }
        if (Math.abs(row - from.row) > 1 || Math.abs(col - from.col) > 1) {
          setCardMsg('🧬 Must be an ADJACENT square!');
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const src = nb[from.row][from.col]!;
        nb[row][col] = { ...src };
        const kp  = findKing(nb, playerColor);
        const okp = findKing(nb, opp);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🧬 Clone would leave your king in check!');
          return;
        }
        if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
          setCardMsg('🧬 Cannot clone there — would put enemy king in check!');
          return;
        }

        triggerCloneAnim?.(from, { row, col }, src.type, playerColor);
        playMoveSound('move');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🧬 Cloned ${src.type} to ${FILES[col]}${RANKS[row]}!`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Clone failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🧬 Cloned ${src.type} to ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── MIND CONTROL ───
    if (mechanic === 'mindcontrol') {
      if (!piece || piece.color !== opp || piece.type === 'king') {
        setCardMsg('🧠 Click an ENEMY piece to steal (not king)');
        return;
      }
      const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
      nb[row][col] = { ...piece, color: playerColor };
      const kp  = findKing(nb, playerColor);
      const okp = findKing(nb, opp);
      if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
        setCardMsg('🧠 Cannot steal — would leave your king in check!');
        return;
      }
      if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
        setCardMsg('🧠 Cannot steal — would put enemy king in check!');
        return;
      }

      triggerMindControlAnim({ row, col }, playerColor, piece.type);
      fireCardAnim('mindcontrol', `Controlled ${piece.type}`);
      playCardSound('mindcontrol');

      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🧠 Stole enemy ${piece.type}! It's yours now.`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Mind control failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
      } else {
        setBoard(nb);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🧠 Stole enemy ${piece.type}! It's yours now.`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── BORROW ───
    if (mechanic === 'borrow') {
      if (!piece || piece.color !== opp || piece.type === 'king') {
        setCardMsg('🤏 Click an ENEMY piece to borrow for 1 turn (not king)');
        return;
      }
      const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
      nb[row][col] = { ...piece, color: playerColor, borrowed: true };
      const kp  = findKing(nb, playerColor);
      const okp = findKing(nb, opp);
      if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
        setCardMsg('🤏 Cannot borrow — would leave your king in check!');
        return;
      }
      if (okp && isAttackedWithFusion(nb, okp.row, okp.col, playerColor)) {
        setCardMsg('🤏 Cannot borrow — would put enemy king in check!');
        return;
      }

      fireCardAnim('smallsacrifice', `Borrowed ${piece.type}`);
      playCardSound('borrow');

      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🤏 Borrowed enemy ${piece.type} for this turn!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Borrow failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
      } else {
        setBoard(nb);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🤏 Borrowed enemy ${piece.type} for this turn!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── PARASITE ───
    if (mechanic === 'parasite') {
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king') {
          setCardMsg('🦠 Click YOUR piece to be the host (not king)');
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot parasitize with a frozen piece!');
          return;
        }
        const val = PIECE_VALUE[piece.type];
        setCardMsg(`🦠 Now click an ENEMY piece of SAME VALUE (${val} pts) to link`);
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, hostSq: { row, col }, hostValue: val } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      if (step === 2) {
        const hostSq = (data.hostSq as Sq) || (data.sq as Sq) || (cardPending.data?.hostSq as Sq) || (cardPending.data?.sq as Sq);
        if (!hostSq) {
          setCardMsg('🦠 Click YOUR host piece first');
          return;
        }
        if (!piece || piece.color !== opp || piece.type === 'king') {
          setCardMsg('🦠 Must pick an ENEMY piece (not king)!');
          return;
        }
        const hostVal = data.hostValue as number | undefined;
        if (hostVal !== undefined && PIECE_VALUE[piece.type] !== hostVal) {
          setCardMsg(`🦠 Must pick an enemy piece with SAME value (${hostVal} pts)!`);
          return;
        }
        if (piece.frozen) {
          setCardMsg('❄️ Cannot parasitize a frozen piece!');
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        const hostPiece = nb[hostSq.row]?.[hostSq.col];
        if (!hostPiece) {
          setCardMsg('🦠 Host piece not found');
          return;
        }
        hostPiece.parasiteTarget = `${row},${col}`;
        playCardSound('card_play');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🦠 Parasite linked! If your ${hostPiece.type} dies, their ${piece.type} dies too!`);
            setTimeout(() => setCardMsg(''), 3000);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Parasite failed';
            setCardMsg(message);
            setTimeout(() => setCardMsg(''), 2500);
          });
        } else {
          setBoard(nb);
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🦠 Parasite linked! If your ${hostPiece.type} dies, their ${piece.type} dies too!`);
          setTimeout(() => setCardMsg(''), 3000);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── DEMOTE HIM / PROMOTE HIM ───
    if (mechanic === 'demotehim') {
      if (!piece || piece.type === 'king') {
        setCardMsg('📉 Click ANY piece to demote (not king)');
        return;
      }
      const DOWNGRADE: Record<PieceType, PieceType[]> = {
        queen: ['rook', 'bishop', 'knight'],
        rook: ['bishop', 'knight', 'pawn'],
        bishop: ['knight', 'pawn'],
        knight: ['pawn'],
        pawn: [],
        king: [],
      };
      const downgrades = DOWNGRADE[piece.type];
      if (!downgrades?.length) {
        setCardMsg('📉 That piece is already a pawn — cannot demote further!');
        return;
      }
      setPromoPicker({ sq: { row, col }, options: downgrades, mechanic: 'demotehim' });
      setCardMsg('📉 Choose what to demote it to:');
      setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq: { row, col } } } : null);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Target selection failed';
          setCardMsg(message);
        });
      }
      return;
    }

    if (mechanic === 'promotehim') {
      if (!piece || piece.color !== opp || piece.type === 'king') {
        setCardMsg('📈 Click an ENEMY piece to promote (not king)');
        return;
      }
      const UPGRADE: Record<PieceType, PieceType[]> = {
        pawn: ['knight', 'bishop', 'rook', 'queen'],
        knight: ['bishop', 'rook', 'queen'],
        bishop: ['rook', 'queen'],
        rook: ['queen'],
        queen: [],
        king: [],
      };
      const upgrades = UPGRADE[piece.type];
      if (!upgrades?.length) {
        setCardMsg('📈 That piece cannot be promoted further!');
        return;
      }
      setPromoPicker({ sq: { row, col }, options: upgrades, mechanic: 'promotehim' });
      setCardMsg('📈 Choose what to promote enemy piece to:');
      setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq: { row, col } } } : null);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Target selection failed';
          setCardMsg(message);
        });
      }
      return;
    }

    // ─── PROMOTE / DEMOTE ───
    if (mechanic === 'promote') {
      if (!piece || piece.color !== playerColor || piece.type === 'king') {
        setCardMsg('⬆️ Click YOUR piece to promote (not king)');
        return;
      }
      const UPGRADE: Record<PieceType, PieceType[]> = {
        pawn: ['knight', 'bishop', 'rook', 'queen'],
        knight: ['bishop', 'rook', 'queen'],
        bishop: ['rook', 'queen'],
        rook: ['queen'],
        queen: [],
        king: [],
      };
      const upgrades = UPGRADE[piece.type];
      if (!upgrades?.length) {
        setCardMsg('⬆️ That piece cannot be promoted further!');
        return;
      }
      setPromoPicker({ sq: { row, col }, options: upgrades, mechanic: 'promote' });
      setCardMsg('⬆️ Choose what to promote it to:');
      setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq: { row, col } } } : null);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Target selection failed';
          setCardMsg(message);
        });
      }
      return;
    }

    if (mechanic === 'demote') {
      if (!piece || piece.color !== playerColor || piece.type === 'king') {
        setCardMsg('⬇️ Click YOUR piece to demote (not king)');
        return;
      }
      const DOWNGRADE: Record<PieceType, PieceType[]> = {
        queen: ['rook', 'bishop', 'knight'],
        rook: ['bishop', 'knight', 'pawn'],
        bishop: ['knight', 'pawn'],
        knight: ['pawn'],
        pawn: [],
        king: [],
      };
      const downgrades = DOWNGRADE[piece.type];
      if (!downgrades?.length) {
        setCardMsg('⬇️ That piece is already a pawn!');
        return;
      }
      setPromoPicker({ sq: { row, col }, options: downgrades, mechanic: 'demote' });
      setCardMsg('⬇️ Choose what to demote it to:');
      setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq: { row, col } } } : null);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Target selection failed';
          setCardMsg(message);
        });
      }
      return;
    }

    // ─── HALF FUSE & FULL FUSION ───
    if (mechanic === 'halffuse' || mechanic === 'fullfusion') {
      const isHalf = mechanic === 'halffuse';
      const HALF_CAP = 6;
      if (step === 1) {
        if (!piece || piece.color !== playerColor || piece.type === 'king' || piece.fusedWith) {
          setCardMsg(`⚗️ Click YOUR non-fused piece (not king)`);
          return;
        }
        setCardMsg(isHalf ? '⚗️ Now click an ADJACENT own piece to fuse with (combined ≤6 pts)' : '🔮 Now click ANY own piece to fuse with');
        setCardPending(prev => prev ? { ...prev, step: 2, data: { ...prev.data, sq1: { row, col }, type1: piece.type, val1: PIECE_VALUE[piece.type] } } : null);
        void sendAuthoritativeTarget({ row, col }).catch(err => {
          const message = err instanceof Error ? err.message : 'Step 1 selection failed';
          setCardMsg(message);
        });
        return;
      }
      if (step === 2) {
        const sq1 = (data.sq1 as Sq) || (cardPending.data.sq1 as Sq);
        const type1 = (data.type1 as PieceType) || (cardPending.data.type1 as PieceType);
        if (!sq1 || !type1) { setCardMsg('⚗️ Click first piece first'); return; }
        if (!piece || piece.color !== playerColor || piece.type === 'king' || piece.fusedWith) {
          setCardMsg('⚗️ Must click YOUR non-fused piece (not king)!');
          return;
        }
        if (row === sq1.row && col === sq1.col) {
          setCardMsg('⚗️ Pick a different piece to fuse with!');
          return;
        }
        if (isHalf) {
          const adjacent = Math.abs(row - sq1.row) <= 1 && Math.abs(col - sq1.col) <= 1;
          if (!adjacent) { setCardMsg('⚗️ Half Fuse requires ADJACENT pieces!'); return; }
          const combined = (PIECE_VALUE[type1] ?? 0) + (PIECE_VALUE[piece.type] ?? 0);
          if (combined > HALF_CAP) { setCardMsg(`⚗️ Combined value ${combined} exceeds max ${HALF_CAP} pts!`); return; }
        }
        const redundancyErr = checkFusionRedundancy(type1, piece.type);
        if (redundancyErr) { setCardMsg(redundancyErr); return; }

        triggerFuseAnim({ sq1, sq2: { row, col }, type1, type2: piece.type, color: playerColor });
        playMoveSound('capture');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`${isHalf ? '⚗️ Half Fuse' : '🔮 Full Fusion'} completed at ${FILES[col]}${RANKS[row]}!`);
            setTimeout(() => setCardMsg(''), 2500);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Fusion failed';
            setCardMsg(message);
          });
        } else {
          setBoard(prev => {
            const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
            nb[sq1.row][sq1.col] = null;
            nb[row][col] = { ...piece, fusedWith: type1 };
            return nb;
          });
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`${isHalf ? '⚗️ Half Fuse' : '🔮 Full Fusion'} completed at ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
          finishCardUse(card, playerColor);
        }
        return;
      }
    }

    // ─── SMALL SACRIFICE & BIG SACRIFICE ───
    if (mechanic === 'smallsacrifice' || mechanic === 'bigsacrifice') {
      const isBig = mechanic === 'bigsacrifice';
      const targetVal = isBig ? 14 : 6;
      const selected = ((data.selected as Sq[] | undefined) ?? []).slice();
      const currentVal = selected.reduce((sum, s) => sum + (PIECE_VALUE[b[s.row][s.col]?.type ?? 'pawn'] ?? 0), 0);

      // Clicking empty square confirms sacrifice
      if (!piece) {
        if (currentVal < targetVal) {
          setCardMsg(`🩸 Total value: ${currentVal}/${targetVal}. Keep clicking YOUR pieces to reach ${targetVal}+ pts!`);
          return;
        }
        const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
        for (const s of selected) nb[s.row][s.col] = null;
        const kp = findKing(nb, playerColor);
        if (kp && isAttackedWithFusion(nb, kp.row, kp.col, opp)) {
          setCardMsg('🩸 Cannot sacrifice — would leave your king in check!');
          return;
        }
        triggerSacrificeAnim(selected);
        fireCardAnim('smallsacrifice', isBig ? 'Big Sacrifice' : 'Small Sacrifice');
        playMoveSound('capture');

        if (authoritativeMatchIdRef.current) {
          void sendAuthoritativeTarget({ row, col }).then(() => {
            setCardPending(null);
            setSelectedCard(null);
            setCardMsg(`🩸 Sacrificed ${selected.length} piece(s) (${currentVal} pts)!`);
            setTimeout(() => setCardMsg(''), 3000);
          }).catch(err => {
            const message = err instanceof Error ? err.message : 'Sacrifice failed';
            setCardMsg(message);
          });
        } else {
          setBoard(nb);
          const drawCount = isBig ? 3 : 2;
          const drawnCards = Array.from({ length: drawCount }, () => CARD_POOL[Math.floor(Math.random() * CARD_POOL.length)]);
          const addFn = (h: GameCard[]) => {
            let nextH = [...h];
            for (const c of drawnCards) {
              if (nextH.length < 10) nextH.push(c);
            }
            return nextH;
          };
          if (playerColor === 'white') setWhiteHand(addFn);
          else setBlackHand(addFn);

          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🩸 Sacrificed ${selected.length} piece(s) (${currentVal} pts)! Drew ${drawnCards.map(c => c.name).join(' + ')}`);
          setTimeout(() => setCardMsg(''), 3500);
          finishCardUse(card, playerColor);
        }
        return;
      }

      if (piece.color !== playerColor || piece.type === 'king') {
        setCardMsg(`🩸 Click YOUR pieces to sacrifice (not king). Click empty square when done.`);
        return;
      }

      const existingIdx = selected.findIndex(s => s.row === row && s.col === col);
      const nextSelected = existingIdx >= 0
        ? selected.filter((_, i) => i !== existingIdx)
        : [...selected, { row, col }];
      const nextVal = nextSelected.reduce((sum, s) => sum + (PIECE_VALUE[b[s.row][s.col]?.type ?? 'pawn'] ?? 0), 0);

      setCardPending(prev => prev ? { ...prev, data: { ...prev.data, selected: nextSelected } } : null);
      setCardMsg(`🩸 Selected ${nextSelected.length} piece(s) = ${nextVal} pts (need ${targetVal}+). Click empty square to confirm.`);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).catch(() => {});
      }
      return;
    }

    // ─── BLACK HOLE ───
    if (mechanic === 'blackhole') {
      triggerBlackHoleAnim?.({ row, col });
      fireCardAnim('blackhole', 'Black Hole');
      playMoveSound('bomb');

      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🕳️ Black Hole consumed 3x3 at ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Black hole failed';
          setCardMsg(message);
        });
      } else {
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          for (let dr = -1; dr <= 1; dr++) {
            for (let dc = -1; dc <= 1; dc++) {
              const r = row + dr, c = col + dc;
              if (r >= 0 && r < 8 && c >= 0 && c < 8) {
                if (nb[r][c]?.type !== 'king') nb[r][c] = null;
              }
            }
          }
          return nb;
        });
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🕳️ Black Hole consumed 3x3 at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── FAKE PIECE (DECOY) ───
    if (mechanic === 'fakepiece') {
      if (piece) { setCardMsg('🎭 Must click an EMPTY square to place fake piece!'); return; }
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🎭 Decoy placed at ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Decoy placement failed';
          setCardMsg(message);
        });
      } else {
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = { type: 'pawn', color: playerColor, fake: true };
          return nb;
        });
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🎭 Decoy placed at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── FREEZE ───
    if (mechanic === 'freeze') {
      if (!piece || piece.color !== opp || piece.type === 'king') return;
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`❄️ Frozen ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Freeze failed';
          setCardMsg(message);
        });
      } else {
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = { ...piece, frozen: true };
          return nb;
        });
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`❄️ Frozen ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── SHIELD ───
    if (mechanic === 'shield') {
      if (!piece || piece.color !== playerColor || piece.type === 'king') return;
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🛡️ Shielded ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Shield failed';
          setCardMsg(message);
        });
      } else {
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = { ...piece, shielded: true, shieldTurn: 0 };
          return nb;
        });
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🛡️ Shielded ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── SNIPER ───
    if (mechanic === 'sniper') {
      if (!piece || piece.type === 'king') return;
      triggerSniperAnim({ row, col }, piece.type, piece.color, 'sniper');
      fireCardAnim('sniper', `${piece.type} eliminated`);
      playMoveSound('capture');

      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🎯 Sniper eliminated ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Sniper failed';
          setCardMsg(message);
        });
      } else {
        setTimeout(() => {
          setBoard(prev => {
            const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
            nb[row][col] = null;
            return nb;
          });
        }, 1100);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🎯 Sniper eliminated ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── BAD SNIPER ───
    if (mechanic === 'badsniper') {
      if (!piece || piece.type === 'king' || piece.color !== playerColor) return;
      triggerSniperAnim({ row, col }, piece.type, piece.color, 'badsniper');
      fireCardAnim('sniper', `${piece.type} eliminated`);
      playMoveSound('capture');

      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🎯 Bad Sniper eliminated your own ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Bad sniper failed';
          setCardMsg(message);
        });
      } else {
        setTimeout(() => {
          setBoard(prev => {
            const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
            nb[row][col] = null;
            return nb;
          });
        }, 1100);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🎯 Bad Sniper eliminated your own ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── LAVA GROUND ───
    if (mechanic === 'lavaground') {
      if (piece) {
        setCardMsg('🌋 Must click an EMPTY square to place lava!');
        return;
      }
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🌋 Lava trap placed on ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Lava placement failed';
          setCardMsg(message);
        });
      } else {
        setLavaSquares(prev => [...prev, { row, col, movesLeft: 999 }]);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🌋 Lava trap placed on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── FORTRESS ───
    if (mechanic === 'fortress') {
      const tr = Math.min(row, 6);
      const tc = Math.min(col, 6);
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🏰 Fortress zone placed with top-left at ${FILES[tc]}${RANKS[tr]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Fortress placement failed';
          setCardMsg(message);
        });
      } else {
        setFortressZones(prev => [...prev, { topRow: tr, leftCol: tc, ownerColor: playerColor, turnsLeft: 4 }]);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🏰 Fortress zone placed with top-left at ${FILES[tc]}${RANKS[tr]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── FOG VILLAGE ───
    if (mechanic === 'fog_village') {
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`🌫️ Fog Village placed around ${FILES[col]}${RANKS[row]} for 3 turns!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Fog placement failed';
          setCardMsg(message);
        });
      } else {
        setFogZones(prev => [...prev, { centerRow: row, centerCol: col, ownerColor: playerColor, turnsLeft: 3 }]);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`🌫️ Fog Village placed around ${FILES[col]}${RANKS[row]} for 3 turns!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── INVISIBLE ───
    if (mechanic === 'invisible') {
      if (!piece || piece.color !== playerColor || piece.type === 'king') return;
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`👻 ${piece.type} turned invisible for 3 turns!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Invisible failed';
          setCardMsg(message);
        });
      } else {
        setGhostPiece({ piece, row, col, ownerColor: playerColor, roundsLeft: 3 });
        if (ghostRef) ghostRef.current = { piece, row, col, ownerColor: playerColor, roundsLeft: 3 };
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = null;
          return nb;
        });
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`👻 ${piece.type} turned invisible for 3 turns!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // ─── UNABOMBER ───
    if (mechanic === 'unabomber') {
      if (!piece || piece.color !== playerColor || piece.type === 'king') return;
      if (authoritativeMatchIdRef.current) {
        void sendAuthoritativeTarget({ row, col }).then(() => {
          setCardPending(null);
          setSelectedCard(null);
          setCardMsg(`💣 Bomb attached to ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
          setTimeout(() => setCardMsg(''), 2500);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Bomb placement failed';
          setCardMsg(message);
        });
      } else {
        setBombPieces(prev => [...prev, { row, col, ownerColor: playerColor, turnsLeft: 3 }]);
        setCardPending(null);
        setSelectedCard(null);
        setCardMsg(`💣 Bomb attached to ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
      }
      return;
    }

    // Default fallback
    setCardMsg('Card processed');
    setTimeout(() => setCardMsg(''), 2000);
    finishCardUse(card, playerColor);
  }, [
    cardPending, board, authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot,
    triggerSniperAnim, triggerSwapAnim, triggerTeleportAnim, triggerJumpAnim, triggerMindControlAnim,
    triggerSacrificeAnim, triggerCloneAnim, triggerBlackHoleAnim, triggerFuseAnim, checkFusionRedundancy,
    finishCardUse, setBoard, setCardMsg, setPromoPicker, setFortressZones, setFogZones, setGhostPiece,
    ghostRef, setLavaSquares, setBombPieces, setSelectedCard, setWhiteHand, setBlackHand,
    isAttackedWithFusion, playMoveSound, playCardSound, fireCardAnim, setCardPending
  ]);

  const applyCard = React.useCallback((card: GameCard, playerColor: PieceColor) => {
    if (!canUseCard(card, playerColor)) return;
    if (pendingCardUseRef.current.has(card.id)) return;

    if (card.mechanic === 'joker') {
      if (authoritativeMatchIdRef.current) {
        pendingCardUseRef.current.add(card.id);
        const jokerIntent: Omit<Extract<PlayerIntent, { type: 'play_card' }>, 'matchId'> = {
          type: 'play_card',
          ...authoritativeActorForColor(playerColor),
          cardId: card.id,
        };
        void applyIntent(authoritativeMatchIdRef.current, jokerIntent).then(snapshot => {
          applyAuthoritativeSnapshot(snapshot);
          openJokerPicker(card, playerColor);
          setCardMsg('🃏 Choose a backend-supported transformation for Joker.');
        }).catch(err => {
          pendingCardUseRef.current.delete(card.id);
          const message = err instanceof Error ? err.message : 'Joker activation failed';
          setCardMsg(message);
          setTimeout(() => setCardMsg(''), 2500);
        });
        return;
      }
      openJokerPicker(card, playerColor);
      return;
    }

    pendingCardUseRef.current.add(card.id);

    if (authoritativeMatchIdRef.current && (card.mechanic === 'freeze' || card.mechanic === 'shield' || card.mechanic === 'sniper' || card.mechanic === 'badsniper' || card.mechanic === 'promote' || card.mechanic === 'demote' || card.mechanic === 'promotehim' || card.mechanic === 'demotehim' || card.mechanic === 'teleport' || card.mechanic === 'jump' || card.mechanic === 'doublemove_diff' || card.mechanic === 'doublemove_same' || card.mechanic === 'swapme' || card.mechanic === 'swapus' || card.mechanic === 'swaphim' || card.mechanic === 'borrow' || card.mechanic === 'mindcontrol' || card.mechanic === 'parasite' || card.mechanic === 'clone' || card.mechanic === 'fakepiece' || card.mechanic === 'smallsacrifice' || card.mechanic === 'bigsacrifice' || card.mechanic === 'gambler' || card.mechanic === 'radar' || card.mechanic === 'cheater' || card.mechanic === 'lavaground' || card.mechanic === 'blackhole' || card.mechanic === 'fortress' || card.mechanic === 'fog_village' || card.mechanic === 'invisible' || card.mechanic === 'unabomber' || card.mechanic === 'halffuse' || card.mechanic === 'fullfusion' || card.mechanic === 'reverse' || card.mechanic === 'undo' || card.mechanic === 'mirror')) {
      const playCardIntent: Omit<Extract<PlayerIntent, { type: 'play_card' }>, 'matchId'> = {
        type: 'play_card',
        ...authoritativeActorForColor(playerColor),
        cardId: card.id
      };

      void applyIntent(authoritativeMatchIdRef.current, playCardIntent).then(snapshot => {
        applyAuthoritativeSnapshot(snapshot);
        pendingCardUseRef.current.delete(card.id);
        if (card.mechanic === 'doublemove_diff') {
          setCardMsg('Twin active! Make your first move, then move a different piece.');
        } else if (card.mechanic === 'doublemove_same') {
          setCardMsg('Solo active! Make your first move, then move the same piece again.');
        } else if (card.mechanic === 'reverse') {
          setCardMsg("Reversed opponent's last move!");
          fireCardAnim('reverse', "Opponent's last move undone");
        } else if (card.mechanic === 'undo') {
          setCardMsg("Undo armed! Opponent's next card will be nullified.");
        } else if (card.mechanic === 'mirror') {
          setCardMsg('Mirror resolved.');
        } else if (card.mechanic === 'gambler') {
          const eventList = snapshot.events ?? [];
          const lastEvent = [...eventList].reverse().find(event => event.type === 'card_played') ?? eventList[eventList.length - 1];
          const outcome = lastEvent?.payload?.outcome;
          const affectedCard = lastEvent?.payload?.card as GameCard | undefined;
          if (outcome === 'win' && affectedCard) {
            setCardMsg(`🎲 WIN! Stole "${affectedCard.name}" from opponent!`);
            fireCardAnim('gambler_win', `Stole "${affectedCard.name}" ${affectedCard.icon}`);
          } else if (outcome === 'lose' && affectedCard) {
            setCardMsg(`🎲 LOSE! Gave "${affectedCard.name}" to opponent...`);
            fireCardAnim('gambler_lose', `Gave away "${affectedCard.name}" ${affectedCard.icon}`);
          } else {
            setCardMsg('🎲 Gambler had no effect.');
          }
        } else if (card.mechanic === 'radar') {
          setCardMsg('📡 Radar active! Enemy hand revealed for this turn.');
        } else if (card.mechanic === 'cheater') {
          analyse(
            toFEN(snapshot.match.board as Board, snapshot.match.turn, new Set(snapshot.match.moved), snapshot.match.lastMove, snapshot.match.halfMoveClock, snapshot.match.fullMoveNumber),
            snapshot.match.turn,
          );
          setCardMsg('💡 Cheater active for 3 turns! Engine panel shows best move.');
        } else if (card.mechanic === 'fortress') {
          setCardMsg('🏰 Fortress ready. Click the board to place the 2x2 zone.');
        } else {
          setCardMsg(CARD_TARGET_MESSAGES[card.mechanic] ?? 'Click a square...');
        }
      }).catch(err => {
        pendingCardUseRef.current.delete(card.id);
        const message = err instanceof Error ? err.message : 'Card play failed';
        setCardMsg(message);
        setTimeout(() => setCardMsg(''), 2000);
      });
      setSelectedCard(null);
      return;
    }

    if (TARGETING_CARDS.has(card.mechanic)) {
      if (card.mechanic === 'doublemove_diff') { activateDoubleMove('diff', card, playerColor); return; }
      if (card.mechanic === 'doublemove_same') { activateDoubleMove('same', card, playerColor); return; }
      setCardPending({ card, playerColor, mechanic: card.mechanic, step: 1, data: {} });
      setCardMsg(CARD_TARGET_MESSAGES[card.mechanic] ?? 'Click a square...');
      setSelectedCard(null);
      return;
    }

    if (card.mechanic === 'gambler') {
      const oppHand = playerColor === 'white' ? blackHand : whiteHand;
      const myHand  = playerColor === 'white' ? whiteHand : blackHand;
      const won = Math.random() < 0.5;
      if (won && oppHand.length > 0) {
        const stolenIdx = Math.floor(Math.random() * oppHand.length);
        const stolenCard = oppHand[stolenIdx];
        removeCardFromHand(stolenCard, OPP[playerColor]);
        if (playerColor === 'white') setWhiteHand(h => [...h, stolenCard]);
        else setBlackHand(h => [...h, stolenCard]);
        setCardMsg(`🎲 WIN! Stole "${stolenCard.name}" from opponent!`);
        fireCardAnim('gambler_win', `Stole "${stolenCard.name}" ${stolenCard.icon}`);
      } else if (!won && myHand.length > 1) {
        const myOtherCards = myHand.filter(c => c.id !== card.id);
        const lostCard = myOtherCards[Math.floor(Math.random() * myOtherCards.length)];
        removeCardFromHand(lostCard, playerColor);
        if (playerColor === 'white') setBlackHand(h => [...h, lostCard]);
        else setWhiteHand(h => [...h, lostCard]);
        setCardMsg(`🎲 LOSE! Gave "${lostCard.name}" to opponent...`);
        fireCardAnim('gambler_lose', `Gave away "${lostCard.name}" ${lostCard.icon}`);
      } else {
        setCardMsg('🎲 Gambler: No effect!');
      }
      setTimeout(() => setCardMsg(''), 3000);
      finishCardUse(card, playerColor);
      return;
    }

    if (card.mechanic === 'radar') {
      setRadarActive(true);
      setCardMsg('📡 Radar active! Enemy hand revealed for this turn.');
      setTimeout(() => setCardMsg(''), 4000);
      finishCardUse(card, playerColor);
      return;
    }

    if (card.mechanic === 'cheater') {
      setCardMsg('💡 Cheater active for 3 turns! Engine panel shows best move.');
      setTimeout(() => setCardMsg(''), 4000);
      finishCardUse(card, playerColor);
      return;
    }

    finishCardUse(card, playerColor);
  }, [canUseCard, pendingCardUseRef, authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot, openJokerPicker, setCardMsg, fireCardAnim, analyse, activateDoubleMove, setCardPending, setSelectedCard, blackHand, whiteHand, removeCardFromHand, finishCardUse, setWhiteHand, setBlackHand, setRadarActive]);

  const getCardHighlight = React.useCallback((row: number, col: number): string | null => {
    if (!cardPending) return null;
    const { mechanic, step, playerColor, data } = cardPending;
    const piece = board[row][col];
    const opp   = OPP[playerColor];
    switch (mechanic) {
      case 'freeze':     return piece?.color === opp && piece.type !== 'king' ? 'rgba(96,165,250,0.55)' : null;
      case 'shield':     return piece?.color === playerColor && piece.type !== 'king' ? 'rgba(74,222,128,0.55)' : null;
      case 'sniper':     return piece && piece.type !== 'king' ? 'rgba(192,132,252,0.55)' : null;
      case 'badsniper':  return piece?.color === playerColor && piece.type !== 'king' ? 'rgba(107,114,128,0.55)' : null;
      case 'mindcontrol':
      case 'borrow':     return piece?.color === opp && piece.type !== 'king' ? 'rgba(168,85,247,0.5)' : null;
      case 'promote':
      case 'demote':     return step === 1 && piece?.color === playerColor && piece.type !== 'king' ? 'rgba(245,158,11,0.55)' : null;
      case 'jump': {
        const halfStart = playerColor === 'white' ? 0 : 4;
        const halfEnd   = playerColor === 'white' ? 3 : 7;
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(74,222,128,0.55)';
        if (step === 2) {
          const from = data.from as Sq | undefined;
          if (from && row === from.row && col === from.col) return 'rgba(245,158,11,0.6)';
          if (!piece && row >= halfStart && row <= halfEnd) return 'rgba(74,222,128,0.35)';
        }
        return null;
      }
      case 'teleport': {
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(192,132,252,0.55)';
        if (step === 2 && !piece) return 'rgba(192,132,252,0.35)';
        if (step === 2) {
          const from = data.from as Sq | undefined;
          if (from && row === from.row && col === from.col) return 'rgba(245,158,11,0.6)';
        }
        return null;
      }
      case 'smallsacrifice':
      case 'bigsacrifice': {
        const selected = (data.selected as Sq[] | undefined) ?? [];
        if (selected.some(s => s.row === row && s.col === col)) return 'rgba(231,76,60,0.7)';
        if (piece?.color === playerColor && piece.type !== 'king') return 'rgba(231,76,60,0.25)';
        return null;
      }
      case 'swapme': {
        const sq1s = data.sq1 as Sq | undefined;
        if (sq1s && row === sq1s.row && col === sq1s.col) return 'rgba(74,222,128,0.85)';
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(74,222,128,0.4)';
        if (step === 2 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(74,222,128,0.5)';
        return null;
      }
      case 'swapus': {
        const sq1s = data.sq1 as Sq | undefined;
        if (sq1s && row === sq1s.row && col === sq1s.col) return 'rgba(74,222,128,0.85)';
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(74,222,128,0.4)';
        if (step === 2 && piece?.color === opp && piece.type !== 'king') return 'rgba(248,113,113,0.5)';
        return null;
      }
      case 'swaphim': {
        const sq1s = data.sq1 as Sq | undefined;
        if (sq1s && row === sq1s.row && col === sq1s.col) return 'rgba(248,113,113,0.85)';
        if (step === 1 && piece?.color === opp && piece.type !== 'king') return 'rgba(248,113,113,0.4)';
        if (step === 2 && piece?.color === opp && piece.type !== 'king') return 'rgba(248,113,113,0.5)';
        return null;
      }
      case 'parasite': {
        const hostSq2 = data.hostSq as Sq | undefined;
        const hostVal = data.hostValue as number | undefined;
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king') return 'rgba(168,85,247,0.5)';
        if (step === 2) {
          if (hostSq2 && row === hostSq2.row && col === hostSq2.col) return 'rgba(168,85,247,0.85)';
          if (piece?.color === opp && piece.type !== 'king' && hostVal !== undefined && PIECE_VALUE[piece.type] === hostVal) return 'rgba(168,85,247,0.5)';
        }
        return null;
      }
      case 'lavaground': return !piece ? 'rgba(255,80,0,0.45)' : null;
      case 'fog_village': return 'rgba(100,180,255,0.22)';
      case 'unabomber':  return step === 1 && piece?.color === playerColor && piece.type !== 'king' ? 'rgba(255,120,30,0.55)' : null;
      case 'invisible':  return piece?.color === playerColor && piece.type !== 'king' ? 'rgba(200,200,255,0.50)' : null;
      case 'halffuse': {
        const HALF_CAP = 6;
        const sq1  = data.sq1 as Sq | undefined;
        const val1 = data.val1 as number | undefined;
        if (step === 1) {
          if (!piece || piece.color !== playerColor || piece.type === 'king' || piece.fusedWith) return null;
          const v = PIECE_VALUE[piece.type];
          return v < HALF_CAP ? 'rgba(251,191,36,0.55)' : 'rgba(251,191,36,0.18)';
        }
        if (step === 2) {
          if (sq1 && row === sq1.row && col === sq1.col) return 'rgba(251,191,36,0.85)';
          if (piece?.color === playerColor && piece.type !== 'king' && !piece.fusedWith) {
            const adjacent = sq1 && Math.abs(row - sq1.row) <= 1 && Math.abs(col - sq1.col) <= 1;
            if (!adjacent) return 'rgba(251,191,36,0.12)';
            const combined = (val1 ?? 0) + PIECE_VALUE[piece.type];
            return combined <= HALF_CAP ? 'rgba(251,191,36,0.55)' : 'rgba(248,113,113,0.35)';
          }
        }
        return null;
      }
      case 'fullfusion': {
        const sq1 = data.sq1 as Sq | undefined;
        if (step === 1) return piece?.color === playerColor && piece.type !== 'king' && !piece.fusedWith ? 'rgba(167,139,250,0.55)' : null;
        if (step === 2) {
          if (sq1 && row === sq1.row && col === sq1.col) return 'rgba(167,139,250,0.85)';
          if (piece?.color === playerColor && piece.type !== 'king' && !piece.fusedWith) {
            const adjacent = sq1 && Math.abs(row - sq1.row) <= 1 && Math.abs(col - sq1.col) <= 1;
            if (!adjacent) return 'rgba(167,139,250,0.12)';
            return 'rgba(167,139,250,0.55)';
          }
        }
        return null;
      }
      default: return null;
    }
  }, [cardPending, board]);

  const getDoubleMoveHighlight = React.useCallback((row: number, col: number): string | null => {
    if (!doubleMove?.trackedSq || doubleMove.movesLeft !== 1) return null;
    const ts = doubleMove.trackedSq;
    if (doubleMove.type === 'same' && row === ts.row && col === ts.col) return 'rgba(74,222,128,0.7)';
    if (doubleMove.type === 'diff' && row === ts.row && col === ts.col) return 'rgba(231,76,60,0.6)';
    return null;
  }, [doubleMove]);

  return {
    cancelCard,
    getSafeTransforms,
    getFusedMoves,
    checkFusionRedundancy,
    activateDoubleMove,
    openJokerPicker,
    applyJokerTransform,
    handlePromoPick,
    canUseCard,
    handleCardClick,
    applyCard,
    getCardHighlight,
    getDoubleMoveHighlight,
  };
}
