import type { Board, PieceColor } from './types';
import { isAttacked } from '@chess404/game-core';

export {
  KNIGHT_DELTAS,
  KING_DELTAS,
  PIECE_FEN_MAP,
  anyLegal,
  attacks,
  b2s,
  cloneBoard,
  findKing,
  gameStatus,
  inB,
  insuffMat,
  isAttacked,
  legalMoves,
  makeBoard,
  moveNotation,
  positionKey,
  pseudoMoves,
  threefold,
  toFEN,
  uciToSan,
} from '@chess404/game-core';

export function isAttackedWithFusion(b: Board, row: number, col: number, byColor: PieceColor): boolean {
  if (isAttacked(b, row, col, byColor)) return true;
  for (let r = 0; r < 8; r++) {
    for (let c = 0; c < 8; c++) {
      const p = b[r]?.[c];
      if (!p || p.color !== byColor || !p.fusedWith) continue;
      const tempBoard: Board = b.map(row2 => row2.map(p2 => p2 ? { ...p2 } : null));
      tempBoard[r][c] = { ...p, type: p.fusedWith, fusedWith: undefined };
      if (isAttacked(tempBoard, row, col, byColor)) return true;
    }
  }
  return false;
}

