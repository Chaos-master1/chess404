'use client';

import React from 'react';
import type {
  Board,
  PieceType,
  PieceColor,
  Piece,
  Sq,
  GameCard,
  CardMechanic,
  CardPendingState,
  DoubleMove,
  Snapshot,
} from '../types';
import type { MatchSnapshotMessage, PlayerIntent } from '@chess404/contracts';
import { applyIntent, fetchMatch } from '../lib/match-service';
import {
  findKing,
  isAttacked,
  legalMoves,
  gameStatus,
  insuffMat,
  positionKey,
  threefold,
  toFEN,
  moveNotation,
  cloneBoard,
} from '../chessEngine';
import {
  OPP,
  FILES,
  RANKS,
} from '../constants';

export interface UseBoardMoveHandlerProps {
  board: Board;
  setBoard: React.Dispatch<React.SetStateAction<Board>>;
  turn: PieceColor;
  setTurn: React.Dispatch<React.SetStateAction<PieceColor>>;
  moved: Set<string>;
  setMoved: React.Dispatch<React.SetStateAction<Set<string>>>;
  lm: { from: Sq; to: Sq } | null;
  setLm: React.Dispatch<React.SetStateAction<{ from: Sq; to: Sq } | null>>;
  sel: Sq | null;
  setSel: React.Dispatch<React.SetStateAction<Sq | null>>;
  hints: Sq[];
  setHints: React.Dispatch<React.SetStateAction<Sq[]>>;
  drag: Sq | null;
  setDrag: React.Dispatch<React.SetStateAction<Sq | null>>;
  dragPos: { x: number; y: number } | null;
  setDragPos: React.Dispatch<React.SetStateAction<{ x: number; y: number } | null>>;
  promo: { row: number; col: number; color: PieceColor; fromCol?: number; from?: Sq; to?: Sq; authoritativeMatchId?: string; moved: Set<string>; lm: { from: Sq; to: Sq } | null; hmc: number; fmn: number; turn?: PieceColor; note?: string } | null;
  setPromo: React.Dispatch<React.SetStateAction<any>>;
  check: boolean;
  setCheck: React.Dispatch<React.SetStateAction<boolean>>;
  mate: boolean;
  setMate: React.Dispatch<React.SetStateAction<boolean>>;
  stale: boolean;
  setStale: React.Dispatch<React.SetStateAction<boolean>>;
  insuf: boolean;
  setInsuf: React.Dispatch<React.SetStateAction<boolean>>;
  hmc: number;
  setHmc: React.Dispatch<React.SetStateAction<number>>;
  fmn: number;
  setFmn: React.Dispatch<React.SetStateAction<number>>;
  posHist: string[];
  setPosHist: React.Dispatch<React.SetStateAction<string[]>>;
  drawOffer: PieceColor | null;
  setDrawOffer: React.Dispatch<React.SetStateAction<PieceColor | null>>;
  over: boolean;
  setOver: React.Dispatch<React.SetStateAction<boolean>>;
  winner: PieceColor | 'draw' | 'aborted' | null;
  setWinner: React.Dispatch<React.SetStateAction<PieceColor | 'draw' | 'aborted' | null>>;
  movHist: any[];
  setMovHist: React.Dispatch<React.SetStateAction<any[]>>;
  snapshots: Snapshot[];
  setSnapshots: React.Dispatch<React.SetStateAction<Snapshot[]>>;
  analysisArrows: { from: Sq; to: Sq }[];
  setAnalysisArrows: React.Dispatch<React.SetStateAction<any>>;
  boardRef: React.MutableRefObject<Board>;
  turnRef: React.MutableRefObject<PieceColor>;
  movedRef: React.MutableRefObject<Set<string>>;
  lmRef: React.MutableRefObject<{ from: Sq; to: Sq } | null>;
  hmcRef: React.MutableRefObject<number>;
  fmnRef: React.MutableRefObject<number>;
  posHistRef: React.MutableRefObject<string[]>;
  overRef: React.MutableRefObject<boolean>;
  premoveRef: React.MutableRefObject<{ from: Sq; to: Sq } | null>;
  setPremove: React.Dispatch<React.SetStateAction<{ from: Sq; to: Sq } | null>>;
  doubleMove: DoubleMove | null;
  setDoubleMove: React.Dispatch<React.SetStateAction<DoubleMove | null>>;
  doubleMoveRef: React.MutableRefObject<DoubleMove | null>;
  cardPending: CardPendingState;
  selectedCard: GameCard | null;
  promoPicker: { sq: Sq; options: PieceType[]; mechanic: CardMechanic } | null;
  cardPromo: { sq: Sq; color: PieceColor } | null;
  jokerPicker: any;
  ghostRef: React.MutableRefObject<{ piece: Piece; row: number; col: number; ownerColor: PieceColor; roundsLeft: number } | null>;
  setGhostPiece: React.Dispatch<React.SetStateAction<any>>;
  hostedRuntime: boolean | null;
  viewerSeatRef: React.MutableRefObject<PieceColor | null>;
  authoritativeMatchIdRef: React.MutableRefObject<string | null>;
  authoritativeActorForColor: (color: PieceColor) => { playerId: string; playerSecret?: string; playerClaimToken?: string };
  applyAuthoritativeSnapshot: (snapshot: MatchSnapshotMessage) => void;
  resetCardUsed: (nextTurn: PieceColor) => void;
  startAbortCountdown: () => void;
  stopAbortCountdown: () => void;
  setTicking: (color: PieceColor | null) => void;
  setClockActive: (active: boolean) => void;
  handleLavaLanding: (row: number, col: number, pieceType: PieceType) => void;
  finalPositionRef: React.MutableRefObject<{ fen: string; turn: PieceColor } | null>;
  blackMovedRef: React.MutableRefObject<boolean>;
  setCardMsg: React.Dispatch<React.SetStateAction<string>>;
  handleCardClick: (row: number, col: number) => void;
  isReviewing: boolean;
  getFusedMoves: (b: Board, row: number, col: number, type1: PieceType, type2: PieceType) => Sq[];
}

export function useBoardMoveHandler(props: UseBoardMoveHandlerProps) {
  const {
    board, setBoard, turn, setTurn, moved, setMoved, lm, setLm, sel, setSel,
    hints, setHints, drag, setDrag, dragPos, setDragPos, promo, setPromo,
    check, setCheck, mate, setMate, stale, setStale, insuf, setInsuf,
    hmc, setHmc, fmn, setFmn, posHist, setPosHist, drawOffer, setDrawOffer,
    over, setOver, winner, setWinner, movHist, setMovHist, snapshots, setSnapshots,
    analysisArrows, setAnalysisArrows, boardRef, turnRef, movedRef, lmRef,
    hmcRef, fmnRef, posHistRef, overRef, premoveRef, setPremove, doubleMove,
    setDoubleMove, doubleMoveRef, cardPending, selectedCard, promoPicker, cardPromo,
    jokerPicker, ghostRef, setGhostPiece, hostedRuntime, viewerSeatRef,
    authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot,
    resetCardUsed, startAbortCountdown, stopAbortCountdown, setTicking, setClockActive,
    handleLavaLanding, finalPositionRef, blackMovedRef, setCardMsg, handleCardClick,
    isReviewing, getFusedMoves
  } = props;

  const isAttackedWithFusion = React.useCallback((b: Board, row: number, col: number, byColor: PieceColor): boolean => {
    if (isAttacked(b, row, col, byColor)) return true;
    for (let r = 0; r < 8; r++) {
      for (let c = 0; c < 8; c++) {
        const p = b[r][c];
        if (!p || p.color !== byColor || !p.fusedWith) continue;
        const tempBoard: Board = b.map(row2 => row2.map(p2 => p2 ? { ...p2 } : null));
        tempBoard[r][c] = { ...p, type: p.fusedWith, fusedWith: undefined };
        if (isAttacked(tempBoard, row, col, byColor)) return true;
      }
    }
    return false;
  }, []);

  const checkEndGame = React.useCallback((
    nb: Board,
    next: PieceColor,
    newMv: Set<string>,
    newLm: { from: Sq; to: Sq } | null,
    newHmc: number,
    newPh: string[],
    posKey: string,
    fen: string,
    t: PieceColor,
  ) => {
    const st = gameStatus(nb, next, newLm, newMv);
    const kp = findKing(nb, next);
    const opp2 = next === 'white' ? 'black' : 'white';
    const fusionCheck = kp ? isAttackedWithFusion(nb, kp.row, kp.col, opp2) : false;
    const isCheck = st.isCheck || fusionCheck;

    let fusionHasLegal = false;
    if (fusionCheck && !st.isMate) {
      outer: for (let r = 0; r < 8; r++) {
        for (let c = 0; c < 8; c++) {
          const p = nb[r][c];
          if (!p || p.color !== next) continue;
          let moves: Sq[];
          if (p.fusedWith) {
            const b1 = nb.map(row => row.map(p2 => p2 ? { ...p2 } : null));
            b1[r][c] = { ...p, type: p.type, fusedWith: undefined };
            const b2 = nb.map(row => row.map(p2 => p2 ? { ...p2 } : null));
            b2[r][c] = { ...p, type: p.fusedWith, fusedWith: undefined };
            const m1 = legalMoves(b1, r, c, newLm, newMv);
            const m2 = legalMoves(b2, r, c, newLm, newMv);
            const seen = new Set<string>();
            moves = [...m1, ...m2].filter(sq => {
              const key = `${sq.row},${sq.col}`;
              if (seen.has(key)) return false;
              seen.add(key); return true;
            });
          } else {
            moves = legalMoves(nb, r, c, newLm, newMv);
          }
          for (const sq of moves) {
            const test = nb.map(row => row.map(p2 => p2 ? { ...p2 } : null));
            test[sq.row][sq.col] = test[r][c];
            test[r][c] = null;
            const myKp = findKing(test, next);
            if (myKp && !isAttackedWithFusion(test, myKp.row, myKp.col, opp2)) {
              fusionHasLegal = true;
              break outer;
            }
          }
        }
      }
    } else {
      fusionHasLegal = !fusionCheck;
    }

    const isFusionMate  = isCheck && !st.isMate && !fusionHasLegal;
    const isMate  = st.isMate  || isFusionMate;
    const isStale = st.isStale;

    setCheck(isCheck);
    setMate(isMate);
    setStale(isStale);
    const im = insuffMat(nb);
    setInsuf(im);
    const isGameOver =
      newHmc >= 100 ||
      threefold(newPh, posKey) ||
      isMate || isStale || im;
    if (isGameOver) {
      finalPositionRef.current = { fen, turn: next };
      setOver(true);
      if      (newHmc >= 100)            setWinner('draw');
      else if (threefold(newPh, posKey)) setWinner('draw');
      else if (isMate)                   setWinner(t);
      else if (isStale || im)            setWinner('draw');
    }
  }, [isAttackedWithFusion, finalPositionRef, setCheck, setInsuf, setMate, setOver, setStale, setWinner]);

  const canSubmitAuthoritativeMove = React.useCallback((fr: number, fc: number, tr: number, tc: number) => {
    const matchId = authoritativeMatchIdRef.current;
    if (!matchId) return false;
    if (cardPending || selectedCard || promo || promoPicker || cardPromo || jokerPicker) return false;
    if (ghostRef.current) return false;

    const piece = boardRef.current[fr]?.[fc];
    const target = boardRef.current[tr]?.[tc];
    if (!piece) return false;
    if (hostedRuntime) {
      if (viewerSeatRef.current !== piece.color) return false;
      if (turnRef.current !== piece.color) return false;
    }
    if (piece.fusedWith || piece.invisible || piece.shielded || piece.frozen) return false;
    if (target?.fusedWith || target?.shielded || target?.invisible) return false;
    return true;
  }, [cardPending, selectedCard, promo, promoPicker, cardPromo, jokerPicker, hostedRuntime, authoritativeMatchIdRef, boardRef, ghostRef, turnRef, viewerSeatRef]);

  const doMove = React.useCallback((fr: number, fc: number, tr: number, tc: number, forcePromo?: PieceType) => {
    if (overRef.current) return;
    const matchId = authoritativeMatchIdRef.current;
    const liveBoard = boardRef.current;
    const livePiece = liveBoard[fr]?.[fc];
    const liveGhost = ghostRef.current;
    const isAuthoritativePromotion =
      !!matchId &&
      !!livePiece &&
      livePiece.type === 'pawn' &&
      (tr === 0 || tr === 7);

    if (
      matchId &&
      liveGhost &&
      liveGhost.row === fr &&
      liveGhost.col === fc &&
      (!hostedRuntime || (viewerSeatRef.current === liveGhost.ownerColor && turnRef.current === liveGhost.ownerColor))
    ) {
      const backendMoveIntent: Omit<Extract<PlayerIntent, { type: 'make_move' }>, 'matchId'> = {
        type: 'make_move',
        ...authoritativeActorForColor(turnRef.current),
        from: { row: fr, col: fc },
        to: { row: tr, col: tc },
      };

      void applyIntent(matchId, backendMoveIntent).then(snapshot => {
        applyAuthoritativeSnapshot(snapshot);
      }).catch(err => {
        const message = err instanceof Error ? err.message : 'Backend rejected invisible move';
        setCardMsg(`Backend invisible move failed: ${message}`);
        setTimeout(() => setCardMsg(''), 2500);
      });
      return;
    }

    if (isAuthoritativePromotion && !forcePromo) {
      setPromo({
        row: tr,
        col: tc,
        color: livePiece.color,
        fromCol: fc,
        from: { row: fr, col: fc },
        to: { row: tr, col: tc },
        authoritativeMatchId: matchId,
        moved: movedRef.current,
        lm: { from: { row: fr, col: fc }, to: { row: tr, col: tc } },
        hmc: hmcRef.current,
        fmn: fmnRef.current,
        turn: turnRef.current,
      });
      return;
    }

    if (canSubmitAuthoritativeMove(fr, fc, tr, tc)) {
      if (matchId) {
        const backendMoveIntent: Omit<Extract<PlayerIntent, { type: 'make_move' }>, 'matchId'> = {
          type: 'make_move',
          ...authoritativeActorForColor(turnRef.current),
          from: { row: fr, col: fc },
          to: { row: tr, col: tc },
          promotion: forcePromo,
        };

        void applyIntent(matchId, backendMoveIntent).then(snapshot => {
          applyAuthoritativeSnapshot(snapshot);
        }).catch(err => {
          const message = err instanceof Error ? err.message : 'Backend rejected move';
          setCardMsg(`Backend move failed: ${message}`);
          setTimeout(() => setCardMsg(''), 2500);
          void fetchMatch(matchId).then(snapshot => {
            applyAuthoritativeSnapshot(snapshot);
          }).catch(() => {});
        });
        return;
      }
    }
    const b    = boardRef.current;
    const t    = turnRef.current;
    const mv   = movedRef.current;
    const h    = hmcRef.current;
    const f    = fmnRef.current;
    const ph   = posHistRef.current;
    const dm   = doubleMoveRef.current;

    const nb = cloneBoard(b);

    const ghost = ghostRef.current;
    if (ghost && ghost.ownerColor === t && ghost.row === fr && ghost.col === fc) {
      const newGhost = { ...ghost, row: tr, col: tc };
      setGhostPiece(newGhost);
      ghostRef.current = newGhost;

      const testBoard = cloneBoard(b);
      testBoard[tr][tc] = ghost.piece;
      const oppKp = findKing(testBoard, OPP[t]);
      const givesCheck = !!(oppKp && isAttackedWithFusion(testBoard, oppKp.row, oppKp.col, t));
      const targetPiece = nb[tr][tc];
      const isCapture = !!(targetPiece && targetPiece.color !== t);
      const isMove2 = ghost.roundsLeft <= 0;
      if (givesCheck || (isMove2 && isCapture)) {
        const captured = targetPiece;
        nb[tr][tc] = { ...ghost.piece };
        setGhostPiece(null); ghostRef.current = null;
        const reason = givesCheck ? 'giving check' : `captured ${captured?.type}`;
        setCardMsg(`👁️ ${ghost.piece.type} materialised (${reason})!`);
        setTimeout(() => setCardMsg(''), 2500);
      }

      const note2  = `${FILES[fc]}${RANKS[fr]}→${FILES[tc]}${RANKS[tr]}`;
      const newMv2 = new Set(mv).add(`${fr}-${fc}`);
      const newLm2 = { from: { row: fr, col: fc }, to: { row: tr, col: tc } };
      const newFmn2 = t === 'black' ? f + 1 : f;
      const next2: PieceColor = OPP[t];
      setBoard(nb); setMoved(newMv2); setLm(newLm2);
      setFmn(newFmn2); setHmc(h);
      setMovHist(prev => {
        const nx = [...prev];
        if (t === 'white') nx.push({ n: `${nx.length + 1}.`, w: note2 });
        else { const last = nx[nx.length - 1]; if (last && !last.b) nx[nx.length - 1] = { ...last, b: note2 }; }
        return nx;
      });
      resetCardUsed(next2);
      setTurn(next2); setTicking(next2);
      setSel(null); setHints([]);
      return;
    }

    const piece = nb[fr][fc];
    if (!piece) return;

    const cap  = !!nb[tr][tc];
    const isEP = piece.type === 'pawn' && tc !== fc && !nb[tr][tc];

    if (cap && nb[tr][tc]?.shielded) {
      nb[tr][tc] = { ...nb[tr][tc]!, shielded: false };
      setCardMsg('🛡️ Shield blocked the capture!');
      setTimeout(() => setCardMsg(''), 2000);
      return;
    }

    const note = moveNotation(nb, fr, fc, tr, tc, piece, cap || isEP);

    if (piece.type === 'king' && Math.abs(tc - fc) === 2) {
      if (tc === 6) { nb[tr][5] = nb[tr][7]; nb[tr][7] = null; }
      else          { nb[tr][3] = nb[tr][0]; nb[tr][0] = null; }
    }

    if (isEP) nb[fr][tc] = null;
    nb[tr][tc] = { ...piece };
    nb[fr][fc] = null;

    if (cap || isEP) {
      const killedPiece = b[tr][tc];
      if (killedPiece?.parasiteTarget) {
        const [pr, pc] = killedPiece.parasiteTarget.split(',').map(Number);
        if (nb[pr]?.[pc] && nb[pr][pc]!.type !== 'king') {
          const linkedColor = nb[pr][pc]!.color;
          const testNb = cloneBoard(nb);
          testNb[pr][pc] = null;
          const linkedKp = findKing(testNb, linkedColor);
          const linkedOpp = OPP[linkedColor];
          if (linkedKp && isAttackedWithFusion(testNb, linkedKp.row, linkedKp.col, linkedOpp)) {
            setBoard(b);
            setCardMsg(`🦠 Cannot capture — parasite would leave a king in check!`);
            setTimeout(() => setCardMsg(''), 2500);
            setSel(null); setHints([]);
            return;
          }
          nb[pr][pc] = null;
          setCardMsg(`🦠 Parasite triggered! ${killedPiece.type} died → linked piece destroyed too!`);
          setTimeout(() => setCardMsg(''), 3000);
        }
      }
      for (let pr = 0; pr < 8; pr++) {
        for (let pc = 0; pc < 8; pc++) {
          const pp = nb[pr][pc];
          if (pp?.parasiteTarget) {
            const [tpr, tpc] = pp.parasiteTarget.split(',').map(Number);
            if (tpr === tr && tpc === tc) {
              if (pp.type !== 'king') {
                const testNb2 = cloneBoard(nb);
                testNb2[pr][pc] = null;
                const hostKp = findKing(testNb2, pp.color);
                const hostOpp = OPP[pp.color];
                if (hostKp && isAttackedWithFusion(testNb2, hostKp.row, hostKp.col, hostOpp)) {
                  setBoard(b);
                  setCardMsg(`🦠 Cannot capture — parasite would leave a king in check!`);
                  setTimeout(() => setCardMsg(''), 2500);
                  setSel(null); setHints([]);
                  return;
                }
                nb[pr][pc] = null;
                setCardMsg(`🦠 Parasite triggered! Linked enemy piece died → your host piece destroyed too!`);
                setTimeout(() => setCardMsg(''), 3000);
              }
            }
          }
        }
      }
    }

    for (let pr = 0; pr < 8; pr++) {
      for (let pc = 0; pc < 8; pc++) {
        const pp = nb[pr][pc];
        if (pp?.parasiteTarget) {
          const [tpr, tpc] = (pp.parasiteTarget as string).split(',').map(Number);
          if (tpr === fr && tpc === fc) {
            nb[pr][pc] = { ...pp, parasiteTarget: `${tr},${tc}` };
          }
        }
      }
    }

    if (dm?.movesLeft === 2) {
      const oppKp = findKing(nb, OPP[t]);
      if (oppKp && isAttackedWithFusion(nb, oppKp.row, oppKp.col, t)) {
        setCardMsg('🚫 First double move cannot put enemy king in check!');
        setTimeout(() => setCardMsg(''), 2500);
        setSel(null); setHints([]);
        return;
      }
    }

    const newMv  = new Set(mv).add(`${fr}-${fc}`);
    const newLm  = { from: { row: fr, col: fc }, to: { row: tr, col: tc } };
    const newHmc = (piece.type === 'pawn' || cap || isEP) ? 0 : h + 1;
    const newFmn = t === 'black' ? f + 1 : f;

    if (piece.type === 'pawn' && (tr === 7 || tr === 0)) {
      if (forcePromo) {
        nb[tr][tc] = { type: forcePromo, color: piece.color };
      } else {
        setBoard(nb);
        setPromo({ row: tr, col: tc, color: piece.color, fromCol: fc, turn: t, note, moved: newMv, lm: newLm, hmc: newHmc, fmn: newFmn });
        return;
      }
    }

    if (dm?.movesLeft === 1 && dm.trackedSq) {
      const ts = dm.trackedSq;
      if (dm.type === 'same' && (fr !== ts.row || fc !== ts.col)) {
        setCardMsg(`🏃 Solo: must move the SAME piece at ${FILES[ts.col]}${RANKS[ts.row]}!`);
        setTimeout(() => setCardMsg(''), 2500);
        setSel(null); setHints([]);
        return;
      }
      if (dm.type === 'diff' && fr === ts.row && fc === ts.col) {
        setCardMsg('👥 Twin: must move a DIFFERENT piece!');
        setTimeout(() => setCardMsg(''), 2500);
        setSel(null); setHints([]);
        return;
      }
    }

    if (dm && dm.movesLeft > 0) {
      const newMovesLeft = dm.movesLeft - 1;

      if (newMovesLeft > 0) {
        setBoard(nb);
        setMoved(newMv);
        setLm(newLm);
        setHmc(newHmc);
        setFmn(newFmn);

        handleLavaLanding(tr, tc, piece.type);

        const newDm: DoubleMove = { ...dm, movesLeft: newMovesLeft, trackedSq: { row: tr, col: tc }, firstNote: note };
        doubleMoveRef.current = newDm;
        setDoubleMove(newDm);

        setCardMsg(
          dm.type === 'same'
            ? `🏃 Solo: now move the SAME piece again! (${FILES[tc]}${RANKS[tr]})`
            : `👥 Twin: now move a DIFFERENT piece! (not ${FILES[tc]}${RANKS[tr]})`
        );
        setTimeout(() => setCardMsg(''), 4000);
        setSel(null); setHints([]);
        return;
      }

      const firstNote = dm.firstNote ?? '?';
      setMovHist(prev => {
        const nx = [...prev];
        const combined = `${firstNote}+${note}`;
        if (t === 'white') {
          nx.push({ n: `${nx.length + 1}.`, w: combined });
        } else {
          const last = nx[nx.length - 1];
          if (last && !last.b) nx[nx.length - 1] = { ...last, b: combined };
          else nx.push({ n: `${nx.length + 1}.`, b: combined });
        }
        return nx;
      });
      doubleMoveRef.current = null;
      setDoubleMove(null);
    }

    const movedPiece = nb[tr][tc];
    if (movedPiece?.shielded) {
      const oppKingPos = findKing(nb, OPP[t]);
      if (oppKingPos && isAttackedWithFusion(nb, oppKingPos.row, oppKingPos.col, t)) {
        nb[tr][tc] = { ...movedPiece, shielded: false, shieldTurn: undefined };
        setCardMsg('🛡️ Shield shattered — giving check broke the protection!');
        setTimeout(() => setCardMsg(''), 2500);
      }
    }

    const wasDoubleMoveFinal = dm !== null && dm.movesLeft === 1;
    const next: PieceColor = OPP[t];
    resetCardUsed(next);

    const posKey = positionKey(nb, next, newMv, newLm);
    const newPh  = [...ph, posKey];
    const fen    = toFEN(nb, next, newMv, newLm, newHmc, newFmn);
    const snap: Snapshot = { board: nb.map(r => [...r]), turn: next, moved: newMv, lm: newLm, hmc: newHmc, fmn: newFmn, fen };

    setSnapshots(prev => [...prev, snap]);
    setBoard(nb);
    setMoved(newMv);
    setLm(newLm);
    setHmc(newHmc);
    setFmn(newFmn);
    setPosHist(newPh);

    if (!wasDoubleMoveFinal) {
      setMovHist(prev => {
        const nx = [...prev];
        if (t === 'white') nx.push({ n: `${nx.length + 1}.`, w: note });
        else {
          const last = nx[nx.length - 1];
          if (last && !last.b) nx[nx.length - 1] = { ...last, b: note };
        }
        return nx;
      });
    }

    handleLavaLanding(tr, tc, piece.type);

    if (t === 'white' && !blackMovedRef.current) {
      startAbortCountdown();
      setTicking(null);
    } else if (t === 'black' && !blackMovedRef.current) {
      stopAbortCountdown();
      blackMovedRef.current = true;
      setClockActive(true);
      setTicking(next);
    } else {
      setTicking(next);
    }

    setTurn(next);
    checkEndGame(nb, next, newMv, newLm, newHmc, newPh, posKey, fen, t);
    setDrawOffer(null);
  }, [overRef, authoritativeMatchIdRef, boardRef, ghostRef, hostedRuntime, viewerSeatRef, turnRef, authoritativeActorForColor, applyAuthoritativeSnapshot, setCardMsg, setPromo, movedRef, hmcRef, fmnRef, canSubmitAuthoritativeMove, posHistRef, doubleMoveRef, setGhostPiece, setBoard, setMoved, setLm, setFmn, setHmc, setMovHist, resetCardUsed, setTicking, setSel, setHints, handleLavaLanding, setDoubleMove, setSnapshots, setPosHist, blackMovedRef, startAbortCountdown, stopAbortCountdown, setClockActive, setTurn, checkEndGame, setDrawOffer, isAttackedWithFusion]);

  const doPromo = React.useCallback((type: PieceType) => {
    if (!promo) return;
    if (promo.authoritativeMatchId && promo.from && promo.to) {
      const backendMoveIntent: Omit<Extract<PlayerIntent, { type: 'make_move' }>, 'matchId'> = {
        type: 'make_move',
        ...authoritativeActorForColor(promo.turn ?? turn),
        from: promo.from,
        to: promo.to,
        promotion: type,
      };

      void applyIntent(promo.authoritativeMatchId, backendMoveIntent).then(snapshot => {
        setPromo(null);
        applyAuthoritativeSnapshot(snapshot);
      }).catch(err => {
        const message = err instanceof Error ? err.message : 'Backend promotion failed';
        setCardMsg(`Backend promotion failed: ${message}`);
        setTimeout(() => setCardMsg(''), 2500);
      });
      return;
    }

    const nb = cloneBoard(board);
    nb[promo.row][promo.col] = { type, color: promo.color };
    const newMv  = promo.moved;
    const newLm  = promo.lm;
    const newHmc = promo.hmc;
    const t      = promo.turn ?? turn;
    const newFmn = t === 'black' ? promo.fmn + 1 : promo.fmn;
    const PROMO_CHAR: Record<PieceType, string> = { queen:'Q', rook:'R', bishop:'B', knight:'N', king:'', pawn:'' };
    const fullNote = `${promo.note ?? (FILES[promo.col] + RANKS[promo.row])}=${PROMO_CHAR[type]}`;

    setBoard(nb);
    setMoved(newMv);
    setLm(newLm);
    setHmc(newHmc);
    setFmn(newFmn);
    setPromo(null);

    setMovHist(prev => {
      const nx = [...prev];
      if (t === 'white') nx.push({ n: `${nx.length + 1}.`, w: fullNote });
      else {
        const last = nx[nx.length - 1];
        if (last && !last.b) nx[nx.length - 1] = { ...last, b: fullNote };
        else nx.push({ n: `${nx.length + 1}.`, b: fullNote });
      }
      return nx;
    });

    const next: PieceColor = OPP[t];
    resetCardUsed(next);
    const posKey = positionKey(nb, next, newMv, newLm);
    const newPh  = [...posHist, posKey];
    setPosHist(newPh);
    setTicking(next);
    setTurn(next);
    setClockActive(true);

    const fen  = toFEN(nb, next, newMv, newLm, newHmc, newFmn);
    const snap: Snapshot = { board: nb.map(r => [...r]), turn: next, moved: newMv, lm: newLm, hmc: newHmc, fmn: newFmn, fen };
    setSnapshots(prev => [...prev, snap]);

    checkEndGame(nb, next, newMv, newLm, newHmc, newPh, posKey, fen, t);
    if (!over) finalPositionRef.current = { fen, turn: next };
  }, [promo, board, turn, posHist, resetCardUsed, setTicking, checkEndGame, over, applyAuthoritativeSnapshot, authoritativeActorForColor, finalPositionRef, setBoard, setCardMsg, setClockActive, setFmn, setHmc, setLm, setMovHist, setMoved, setPosHist, setPromo, setSnapshots, setTurn]);

  const filterFusionChecks = React.useCallback((
    b: Board,
    moves: Sq[],
    fromRow: number,
    fromCol: number,
    playerColor: PieceColor,
  ): Sq[] => {
    const opp = playerColor === 'white' ? 'black' : 'white';
    return moves.filter(sq => {
      const nb: Board = b.map(r => r.map(p => p ? { ...p } : null));
      nb[sq.row][sq.col] = nb[fromRow][fromCol];
      nb[fromRow][fromCol] = null;
      const kp = findKing(nb, playerColor);
      if (!kp) return true;
      return !isAttackedWithFusion(nb, kp.row, kp.col, opp);
    });
  }, [isAttackedWithFusion]);

  const getMoves = React.useCallback(
    (r: number, c: number) => {
      const ghost = ghostRef.current;
      if (ghost && ghost.ownerColor === turn && ghost.row === r && ghost.col === c) {
        const ghostBoard: Board = board.map(row => row.map(p => p ? { ...p } : null));
        ghostBoard[r][c] = { ...ghost.piece };
        const moves = legalMoves(ghostBoard, r, c, lm, moved);
        return filterFusionChecks(ghostBoard, moves, r, c, turn);
      }
      const p = board[r][c];
      if (p?.fusedWith) {
        const moves = getFusedMoves(board, r, c, p.type, p.fusedWith);
        return filterFusionChecks(board, moves, r, c, p.color);
      }
      const moves = legalMoves(board, r, c, lm, moved);
      return filterFusionChecks(board, moves, r, c, turn);
    },
    [board, lm, moved, turn, getFusedMoves, filterFusionChecks, ghostRef],
  );

  const canControlColor = React.useCallback((color: PieceColor): boolean => {
    if (!hostedRuntime) {
      return true;
    }
    return viewerSeatRef.current === color;
  }, [hostedRuntime, viewerSeatRef]);

  const canActWithColor = React.useCallback((color: PieceColor): boolean => (
    canControlColor(color) && turnRef.current === color
  ), [canControlColor, turnRef]);

  const canSelectPiece = React.useCallback((row: number, col: number): boolean => {
    const dm = doubleMoveRef.current;
    if (!dm || dm.movesLeft === 2) return true;
    const ts = dm.trackedSq;
    if (!ts) return true;
    if (dm.type === 'same' && (row !== ts.row || col !== ts.col)) {
      setCardMsg(`🏃 Solo: move the SAME piece at ${FILES[ts.col]}${RANKS[ts.row]}!`);
      return false;
    }
    if (dm.type === 'diff' && row === ts.row && col === ts.col) {
      setCardMsg('👥 Twin: move a DIFFERENT piece!');
      return false;
    }
    return true;
  }, [doubleMoveRef, setCardMsg]);

  const toggleAnalysisArrow = React.useCallback((from: Sq, to: Sq) => {
    setAnalysisArrows((current: any) => {
      const existingIndex = current.findIndex(
        (arrow: any) =>
          arrow.from.row === from.row &&
          arrow.from.col === from.col &&
          arrow.to.row === to.row &&
          arrow.to.col === to.col,
      );
      if (existingIndex >= 0) {
        return current.filter((_: any, index: number) => index !== existingIndex);
      }
      return [...current, { from, to }];
    });
  }, [setAnalysisArrows]);

  const clearAnalysisArrows = React.useCallback(() => {
    setAnalysisArrows((current: any) => (current.length ? [] : current));
  }, [setAnalysisArrows]);

  const clickSq = React.useCallback((r: number, c: number) => {
    if (cardPending) { handleCardClick(r, c); return; }
    if (isReviewing || over || promo) return;
    const p = board[r][c];
    const ghost = ghostRef.current;
    const isGhostSq = ghost && canActWithColor(ghost.ownerColor) && ghost.row === r && ghost.col === c;
    const myColor = hostedRuntime ? viewerSeatRef.current : turnRef.current;
    const isMyTurn = turnRef.current === myColor;
    const canPremove = hostedRuntime && authoritativeMatchIdRef.current && myColor && !isMyTurn && !overRef.current;

    if (canPremove && !sel) {
      if (p && p.color === myColor && canSelectPiece(r, c)) {
        setSel({ row: r, col: c });
        setHints(getMoves(r, c));
        setCardMsg('🔄 Premove set: click destination');
      }
      return;
    }

    if (canPremove && sel && hints.some(m => m.row === r && m.col === c)) {
      setPremove({ from: sel, to: { row: r, col: c } });
      setSel(null);
      setHints([]);
      setCardMsg('✔ Premove queued');
      setTimeout(() => { if (premoveRef.current) setCardMsg('⏳ Premove will fire when turn starts'); }, 1200);
      return;
    }

    if (!sel) {
      if (isGhostSq || (p && canActWithColor(p.color) && canSelectPiece(r, c))) {
        setSel({ row: r, col: c });
        setHints(getMoves(r, c));
      }
      return;
    }

    if (isGhostSq || (p && canControlColor(p.color))) {
      if (sel.row === r && sel.col === c) { setSel(null); setHints([]); }
      else if ((isGhostSq || canSelectPiece(r, c)) && (!p || canActWithColor(p.color))) { setSel({ row: r, col: c }); setHints(getMoves(r, c)); }
      return;
    }

    if (!hints.some(m => m.row === r && m.col === c)) { setSel(null); setHints([]); return; }

    doMove(sel.row, sel.col, r, c);
    setSel(null);
    setHints([]);
  }, [cardPending, isReviewing, over, promo, board, sel, hints, canSelectPiece, getMoves, doMove, handleCardClick, canActWithColor, canControlColor, hostedRuntime, viewerSeatRef, turnRef, authoritativeMatchIdRef, overRef, setCardMsg, setHints, setPremove, setSel, ghostRef, premoveRef]);

  return {
    isAttackedWithFusion,
    checkEndGame,
    canSubmitAuthoritativeMove,
    doMove,
    doPromo,
    filterFusionChecks,
    getMoves,
    canControlColor,
    canActWithColor,
    canSelectPiece,
    toggleAnalysisArrow,
    clearAnalysisArrows,
    clickSq,
  };
}
