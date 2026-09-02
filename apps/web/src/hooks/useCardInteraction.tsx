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
  playCardSound: (mechanic: CardMechanic) => void;
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
    triggerSniperAnim, triggerTransformAnim, triggerFuseAnim, over, hostedRuntime, viewerSeatRef
  } = props;

  const jokerPickerRef = React.useRef<typeof jokerPicker>(null);
  React.useEffect(() => { jokerPickerRef.current = jokerPicker; }, [jokerPicker]);

  const cancelCard = React.useCallback(() => {
    if (cardPending) pendingCardUseRef.current.delete(cardPending.card.id);
    const jp = jokerPickerRef.current;
    if (jp) pendingCardUseRef.current.delete(jp.card.id);
    setJokerPicker(null);
    setCardPending(null);
    setCardMsg('');
    setPromoPicker(null);
    setCardPromo(null);
    setSelectedCard(null);
  }, [cardPending, pendingCardUseRef, setCardMsg, setCardPending, setCardPromo, setJokerPicker, setPromoPicker, setSelectedCard]);

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
    if (hostedRuntime && viewerSeatRef.current !== playerColor) return false;
    if (card.type !== 'trap' && turn !== playerColor) return false;
    return !cardUsedByRef.current[playerColor];
  }, [over, turn, hostedRuntime, cardUsedByRef, viewerSeatRef]);

  const handleCardClick = React.useCallback((row: number, col: number) => {
    if (!cardPending) return;
    const { card, playerColor, mechanic, step, data } = cardPending;
    const b = board;
    const piece = b[row][col];
    const opp   = OPP[playerColor];

    if (authoritativeMatchIdRef.current && (mechanic === 'freeze' || mechanic === 'shield' || mechanic === 'sniper' || mechanic === 'badsniper' || mechanic === 'promote' || mechanic === 'demote' || mechanic === 'promotehim' || mechanic === 'demotehim' || mechanic === 'teleport' || mechanic === 'jump' || mechanic === 'swapme' || mechanic === 'swapus' || mechanic === 'swaphim' || mechanic === 'borrow' || mechanic === 'mindcontrol' || mechanic === 'parasite' || mechanic === 'clone' || mechanic === 'fakepiece' || mechanic === 'smallsacrifice' || mechanic === 'bigsacrifice' || mechanic === 'lavaground' || mechanic === 'blackhole' || mechanic === 'fortress' || mechanic === 'fog_village' || mechanic === 'invisible' || mechanic === 'unabomber' || mechanic === 'halffuse' || mechanic === 'fullfusion')) {
      const targetIntent: Omit<Extract<PlayerIntent, { type: 'select_target' }>, 'matchId'> = {
        type: 'select_target',
        ...authoritativeActorForColor(playerColor),
        target: { row, col }
      };

      void applyIntent(authoritativeMatchIdRef.current, targetIntent).then(snapshot => {
        applyAuthoritativeSnapshot(snapshot);
        if (mechanic === 'freeze') {
          setCardMsg(`Freeze applied at ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'shield') {
          setCardMsg(`Shield applied at ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'sniper') {
          triggerSniperAnim({ row, col }, piece!.type, piece!.color, 'sniper');
          setCardMsg(`Sniper removed ${piece!.type} on ${FILES[col]}${RANKS[row]}`);
          fireCardAnim('sniper', `${piece!.type} eliminated`);
        } else if (mechanic === 'badsniper') {
          triggerSniperAnim({ row, col }, piece!.type, piece!.color, 'badsniper');
          setCardMsg(`Bad Sniper removed your ${piece!.type} on ${FILES[col]}${RANKS[row]}`);
          fireCardAnim('sniper', `${piece!.type} eliminated`);
        } else if (mechanic === 'promote') {
          setCardMsg(`Choose promotion for ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'promotehim') {
          setCardMsg(`Choose enemy promotion for ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'demotehim') {
          setCardMsg(`Choose demotion for ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'lavaground') {
          setCardMsg(`Lava trap placed on ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'fortress') {
          setCardMsg(`Fortress placed with top-left at ${FILES[Math.min(col, 6)]}${RANKS[Math.min(row, 6)]}`);
        } else if (mechanic === 'fog_village') {
          setCardMsg(`Fog Village placed around ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'invisible') {
          setCardMsg(`Invisible applied to ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'unabomber') {
          setCardMsg(`Bomb attached on ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'halffuse' || mechanic === 'fullfusion') {
          const sq1 = step === 2 ? (data.sq1 as Sq | undefined) : undefined;
          const type1 = data.type1 as PieceType | undefined;
          if (sq1 && type1 && piece) {
            triggerFuseAnim({ sq1, sq2: { row, col }, type1, type2: piece.type, color: playerColor });
            setCardMsg(`${mechanic === 'halffuse' ? 'Half Fuse' : 'Full Fusion'} applied to ${FILES[col]}${RANKS[row]}`);
          } else {
            setCardMsg('Now click an adjacent own piece to fuse');
          }
        } else if (mechanic === 'teleport') {
          const from = step === 2 ? (data.from as Sq | undefined) : undefined;
          if (from) {
            setCardMsg(`Teleported to ${FILES[col]}${RANKS[row]}`);
          } else {
            setCardMsg('Now click destination square');
          }
        } else if (mechanic === 'jump') {
          const from = step === 2 ? (data.from as Sq | undefined) : undefined;
          if (from) {
            setCardMsg(`Jumped to ${FILES[col]}${RANKS[row]}`);
          } else {
            setCardMsg('Now click landing square');
          }
        } else if (mechanic === 'swapme' || mechanic === 'swapus' || mechanic === 'swaphim') {
          const sq1 = step === 2 ? (data.sq1 as Sq | undefined) : undefined;
          if (sq1) {
            setCardMsg(`Swapped ${FILES[sq1.col]}${RANKS[sq1.row]} with ${FILES[col]}${RANKS[row]}`);
          } else {
            setCardMsg('Now click second piece to swap');
          }
        } else if (mechanic === 'borrow' || mechanic === 'mindcontrol') {
          setCardMsg(`Converted piece on ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'parasite') {
          const hostSq = step === 2 ? (data.hostSq as Sq | undefined) : undefined;
          if (hostSq) {
            setCardMsg(`Parasite linked ${FILES[hostSq.col]}${RANKS[hostSq.row]} to ${FILES[col]}${RANKS[row]}`);
          } else {
            setCardMsg('Now click matching enemy piece to infect');
          }
        } else if (mechanic === 'clone') {
          setCardMsg(`Cloned ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'fakepiece') {
          setCardMsg(`Decoy placed at ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'smallsacrifice' || mechanic === 'bigsacrifice') {
          setCardMsg(`Sacrifice completed on ${FILES[col]}${RANKS[row]}`);
        } else if (mechanic === 'blackhole') {
          setCardMsg(`Black hole consumed 3x3 at ${FILES[col]}${RANKS[row]}`);
        }
      }).catch(err => {
        const message = err instanceof Error ? err.message : 'Target selection failed';
        setCardMsg(message);
        setTimeout(() => setCardMsg(''), 2000);
      });
      return;
    }

    // Local / Offline fallback logic for all cards
    switch (mechanic) {
      case 'freeze': {
        if (!piece || piece.color !== opp || piece.type === 'king') return;
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = { ...piece, frozen: true };
          return nb;
        });
        setCardMsg(`❄️ Frozen ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'shield': {
        if (!piece || piece.color !== playerColor || piece.type === 'king') return;
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = { ...piece, shielded: true, shieldTurn: 0 };
          return nb;
        });
        setCardMsg(`🛡️ Shielded ${piece.type} at ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'sniper': {
        if (!piece || piece.type === 'king' || piece.color === playerColor) return;
        triggerSniperAnim({ row, col }, piece.type, piece.color, 'sniper');
        setTimeout(() => {
          setBoard(prev => {
            const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
            nb[row][col] = null;
            return nb;
          });
        }, 1100);
        setCardMsg(`🎯 Sniper eliminated ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        fireCardAnim('sniper', `${piece.type} eliminated`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'badsniper': {
        if (!piece || piece.type === 'king' || piece.color !== playerColor) return;
        triggerSniperAnim({ row, col }, piece.type, piece.color, 'badsniper');
        setTimeout(() => {
          setBoard(prev => {
            const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
            nb[row][col] = null;
            return nb;
          });
        }, 1100);
        setCardMsg(`🎯 Bad Sniper eliminated your own ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        fireCardAnim('sniper', `${piece.type} eliminated`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'lavaground': {
        if (piece) {
          setCardMsg('🌋 Must click an EMPTY square to place lava!');
          setTimeout(() => setCardMsg(''), 2000);
          return;
        }
        setLavaSquares(prev => [...prev, { row, col, movesLeft: 999 }]);
        setCardMsg(`🌋 Lava trap placed on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'fortress': {
        const tr = Math.min(row, 6);
        const tc = Math.min(col, 6);
        setFortressZones(prev => [...prev, { topRow: tr, leftCol: tc, ownerColor: playerColor, turnsLeft: 4 }]);
        setCardMsg(`🏰 Fortress zone placed with top-left at ${FILES[tc]}${RANKS[tr]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'fog_village': {
        setFogZones(prev => [...prev, { centerRow: row, centerCol: col, ownerColor: playerColor, turnsLeft: 3 }]);
        setCardMsg(`🌫️ Fog Village placed around ${FILES[col]}${RANKS[row]} for 3 turns!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'invisible': {
        if (!piece || piece.color !== playerColor || piece.type === 'king') return;
        setGhostPiece({ piece, row, col, ownerColor: playerColor, roundsLeft: 3 });
        if (ghostRef) ghostRef.current = { piece, row, col, ownerColor: playerColor, roundsLeft: 3 };
        setBoard(prev => {
          const nb: Board = prev.map(r => r.map(p => p ? { ...p } : null));
          nb[row][col] = null;
          return nb;
        });
        setCardMsg(`👻 ${piece.type} turned invisible for 3 turns!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      case 'unabomber': {
        if (!piece || piece.color !== playerColor || piece.type === 'king') return;
        setBombPieces(prev => [...prev, { row, col, ownerColor: playerColor, turnsLeft: 3 }]);
        setCardMsg(`💣 Bomb attached to ${piece.type} on ${FILES[col]}${RANKS[row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        finishCardUse(card, playerColor);
        break;
      }
      default: {
        setCardMsg('Card processed');
        setTimeout(() => setCardMsg(''), 2000);
        finishCardUse(card, playerColor);
        break;
      }
    }
  }, [cardPending, board, authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot, triggerSniperAnim, fireCardAnim, triggerFuseAnim, finishCardUse, setBoard, setCardMsg, setFortressZones, setFogZones, setGhostPiece, ghostRef, setLavaSquares, setBombPieces]);

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
      case 'sniper':     return piece && piece.type !== 'king' && piece.color !== playerColor ? 'rgba(192,132,252,0.55)' : null;
      case 'badsniper':  return piece?.color === playerColor && piece.type !== 'king' ? 'rgba(107,114,128,0.55)' : null;
      case 'mindcontrol':
      case 'borrow':     return piece?.color === opp && piece.type !== 'king' ? 'rgba(168,85,247,0.5)' : null;
      case 'promote':
      case 'demote':     return step === 1 && piece?.color === playerColor && piece.type !== 'king' ? 'rgba(245,158,11,0.55)' : null;
      case 'jump': {
        if (step === 1 && piece?.color === playerColor && piece.type !== 'king' && piece.type !== 'knight') return 'rgba(74,222,128,0.55)';
        if (step === 2) {
          const from = data.from as Sq | undefined;
          const pt = data.pieceType as PieceType | undefined;
          const pc = data.pieceColor as PieceColor | undefined;
          if (from && row === from.row && col === from.col) return 'rgba(245,158,11,0.6)';
          if (from && pt && pc && piece?.color !== playerColor) {
            const dr = row - from.row, dc = col - from.col;
            if (dr === 0 && dc === 0) return null;
            const diag = Math.abs(dr) === Math.abs(dc), straight = dr === 0 || dc === 0;
            let dirOk = false;
            if (pt === 'bishop') dirOk = diag;
            else if (pt === 'rook') dirOk = straight;
            else if (pt === 'queen') dirOk = diag || straight;
            else if (pt === 'pawn') {
              const fwd = pc === 'white' ? 1 : -1;
              dirOk = (dc === 0 && (dr === fwd || dr === fwd * 2)) || (Math.abs(dc) === 2 && dr === fwd * 2);
            }
            if (!dirOk) return null;
            const sr = Math.sign(dr), sc = Math.sign(dc);
            let count = 0;
            let r = from.row + sr, c = from.col + sc;
            while (r !== row || c !== col) {
              if (board[r][c]) count++;
              r += sr; c += sc;
            }
            if (count === 1) {
              if (pt === 'pawn' && dc === 0) return !piece ? 'rgba(74,222,128,0.45)' : null;
              if (pt === 'pawn' && Math.abs(dc) === 2) return piece?.type === 'king' ? null : (piece ? 'rgba(248,113,113,0.6)' : 'rgba(74,222,128,0.45)');
              if (piece?.type === 'king') return null;
              return piece ? 'rgba(248,113,113,0.6)' : 'rgba(74,222,128,0.45)';
            }
          }
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
