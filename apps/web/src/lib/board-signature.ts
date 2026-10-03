import type { Board, Piece, PieceColor } from '../types';
import { PIECE_FEN_MAP } from '../chessEngine';

// A compact, deterministic fingerprint of the position the board actually
// paints, rendered into `data-board-signature` so a test can tell a repainted
// board from a frozen one — the board is a single <canvas>, so no DOM
// assertion can see it otherwise.
//
// Never use this value to decide whether to draw: nothing in the render path
// may branch on it, or a field left out here silently drops board updates
// again.
//
// Piece letters come from PIECE_FEN_MAP so a signature can never disagree
// with the FEN the rest of the app prints. The flag list covers every Piece
// field that changes what is drawn; countdowns (shieldTurn, invisibleTurn)
// are folded in by value rather than by presence.

const FLAGS: Array<[keyof Piece, string]> = [
  ['shielded', 'S'],
  ['frozen', 'F'],
  ['invisible', 'I'],
  ['fake', 'K'],
  ['bomb', 'B'],
  ['borrowed', 'R'],
];

function cellToken(piece: Piece | null): string {
  if (!piece) return '.';
  const type = PIECE_FEN_MAP[piece.type];
  const color = piece.color.charAt(0);
  const flags = FLAGS.filter(([key]) => piece[key] === true).map(([, ch]) => ch).join('');
  const fused = piece.fusedWith ? `~${PIECE_FEN_MAP[piece.fusedWith]}` : '';
  const shieldTurn = piece.shieldTurn ? `s${piece.shieldTurn}` : '';
  const invisibleTurn = piece.invisibleTurn ? `i${piece.invisibleTurn}` : '';
  const invisibleOver = piece.invisibleOver ? 'o' : '';
  const parasite = piece.parasiteTarget ? `p${piece.parasiteTarget}` : '';
  const rest = `${flags}${fused}${shieldTurn}${invisibleTurn}${invisibleOver}${parasite}`;
  return `${type}${color}${rest || '-'}`;
}

/**
 * Signature of an 8x8 board plus the side to move. Returns 'invalid' for a
 * malformed board so a caller can assert on a missing/garbage value rather
 * than throwing inside a render.
 */
export function boardSignature(board: Board, turn: PieceColor): string {
  if (!Array.isArray(board) || board.length !== 8) return 'invalid';
  const cells: string[] = [];
  for (let row = 0; row < 8; row++) {
    const line = board[row];
    if (!Array.isArray(line) || line.length !== 8) return 'invalid';
    for (let col = 0; col < 8; col++) {
      cells.push(cellToken(line[col] ?? null));
    }
  }
  return `${turn}|${cells.join(',')}`;
}
