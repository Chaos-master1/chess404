import { describe, expect, it } from 'vitest';
import { boardSignature } from './board-signature';
import type { Board, Piece, PieceType } from '../types';

const ALL_PIECE_TYPES: PieceType[] = ['king', 'queen', 'rook', 'bishop', 'knight', 'pawn'];

function emptyBoard(): Board {
  return Array.from({ length: 8 }, () => Array(8).fill(null) as (Piece | null)[]);
}

function boardWith(piece: Piece, row = 4, col = 4): Board {
  const board = emptyBoard();
  board[row][col] = piece;
  return board;
}

describe('boardSignature', () => {
  it('is stable for two structurally identical boards', () => {
    const a = boardWith({ type: 'knight', color: 'white' });
    const b = boardWith({ type: 'knight', color: 'white' });
    expect(boardSignature(a, 'white')).toBe(boardSignature(b, 'white'));
  });

  it('changes when a piece moves (the frozen-board regression)', () => {
    const before = emptyBoard();
    const after = emptyBoard();
    before[6][4] = { type: 'pawn', color: 'white' };
    after[4][4] = { type: 'pawn', color: 'white' };
    expect(boardSignature(before, 'white')).not.toBe(boardSignature(after, 'white'));
  });

  it('changes when only the side to move changes', () => {
    const board = emptyBoard();
    expect(boardSignature(board, 'white')).not.toBe(boardSignature(board, 'black'));
  });

  // Every flag that changes what is drawn. An earlier ad-hoc comparator in
  // this codebase checked only six of the fourteen Piece fields and silently
  // dropped the rest, so each one is pinned here.
  it.each([
    ['shielded', { shielded: true }],
    ['frozen', { frozen: true }],
    ['invisible', { invisible: true }],
    ['fake', { fake: true }],
    ['bomb', { bomb: true }],
    ['borrowed', { borrowed: true }],
    ['fusedWith', { fusedWith: 'rook' as const }],
    ['shieldTurn countdown', { shielded: true, shieldTurn: 3 }],
    ['invisibleTurn countdown', { invisible: true, invisibleTurn: 2 }],
    ['invisibleOver', { invisible: true, invisibleOver: true }],
    ['parasiteTarget', { parasiteTarget: 'e4' }],
  ])('distinguishes %s', (_label, extra) => {
    const plain = boardWith({ type: 'queen', color: 'black' });
    const decorated = boardWith({ type: 'queen', color: 'black', ...extra });
    expect(boardSignature(plain, 'white')).not.toBe(boardSignature(decorated, 'white'));
  });

  // No two piece types may share a token. 'knight' and 'king' both began
  // with 'k', so swapping one for the other read as "no change" -- exactly
  // the frozen-board regression this value exists to catch.
  it('gives every piece type a token of its own', () => {
    const signatures = ALL_PIECE_TYPES.map(type =>
      boardSignature(boardWith({ type, color: 'white' }), 'white'));
    expect(new Set(signatures).size).toBe(ALL_PIECE_TYPES.length);
  });

  it('gives every fused piece type a token of its own', () => {
    const signatures = ALL_PIECE_TYPES.map(type =>
      boardSignature(boardWith({ type: 'pawn', color: 'white', fusedWith: type }), 'white'));
    expect(new Set(signatures).size).toBe(ALL_PIECE_TYPES.length);
  });

  it('distinguishes piece colour', () => {
    const white = boardWith({ type: 'rook', color: 'white' });
    const black = boardWith({ type: 'rook', color: 'black' });
    expect(boardSignature(white, 'white')).not.toBe(boardSignature(black, 'white'));
  });

  it('returns invalid rather than throwing on a malformed board', () => {
    expect(boardSignature([] as unknown as Board, 'white')).toBe('invalid');
    expect(boardSignature([[null]] as unknown as Board, 'white')).toBe('invalid');
    const shortRow = emptyBoard() as Board;
    shortRow[0] = [null] as unknown as (Piece | null)[];
    expect(boardSignature(shortRow, 'white')).toBe('invalid');
  });
});