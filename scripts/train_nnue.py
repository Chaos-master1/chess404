"""Train NNUE weights for 404-chess engine.

Architecture: 12 binary piece-square (×64) + 5 modifier flags + 74 hand features → 512 → 1.
"""

import struct
import json
import numpy as np
import torch
import torch.nn as nn
import torch.optim as optim
from torch.utils.data import Dataset, DataLoader
from tqdm import tqdm

PIECE_TYPES = 6
COLORS = 2
SQUARES = 64
MODIFIERS = 5
HAND_FEATURES = 74  # 37 mechanics × 2 players
INPUT_SIZE = PIECE_TYPES * COLORS * SQUARES + MODIFIERS + HAND_FEATURES
HIDDEN_SIZE = 512

# Must match Go's mechanicNames order exactly.
MECHANIC_NAMES = [
    "freeze", "shield", "sniper", "badsniper", "teleport", "jump",
    "swapme", "swapus", "swaphim", "clone", "halffuse", "fullfusion",
    "doublemove_same", "doublemove_diff", "promote", "demote", "demotehim", "promotehim",
    "mindcontrol", "borrow", "reverse", "undo", "mirror", "invisible",
    "lavaground", "fog_village", "fortress", "radar",
    "unabomber", "blackhole", "parasite", "fakepiece", "cheater", "gambler",
    "smallsacrifice", "bigsacrifice", "joker",
]

MECHANIC_INDEX = {m: i for i, m in enumerate(MECHANIC_NAMES)}


class NNUE(nn.Module):
    def __init__(self):
        super().__init__()
        self.input_layer = nn.Linear(INPUT_SIZE, HIDDEN_SIZE)
        self.output_layer = nn.Linear(HIDDEN_SIZE, 1)

    def forward(self, x):
        h = self.input_layer(x)
        h = torch.clamp(h, min=0)
        out = self.output_layer(h)
        return out


pieces_map = {
    'P': (0, 0), 'N': (1, 0), 'B': (2, 0), 'R': (3, 0), 'Q': (4, 0), 'K': (5, 0),
    'p': (0, 1), 'n': (1, 1), 'b': (2, 1), 'r': (3, 1), 'q': (4, 1), 'k': (5, 1),
}


def encode_fen(board_fen: str, white_hand=None, black_hand=None):
    """Convert a FEN to NNUE input features (847: 768 piece-square + 5 modifiers + 74 hand)."""
    parts = board_fen.strip().split()
    fen_part = parts[0]
    rows = fen_part.split('/')
    features = np.zeros(INPUT_SIZE, dtype=np.float32)
    # FEN rows are ordered from rank 8 to rank 1. Go board rows use the same order:
    # row 0 = rank 8 (black back rank), row 7 = rank 1 (white back rank)
    for r, row_str in enumerate(rows):
        col = 0
        for ch in row_str:
            if ch.isdigit():
                col += int(ch)
                continue
            if ch in pieces_map:
                ptype, color_idx = pieces_map[ch]
                sq = r * 8 + col  # matches Go board encoding
                idx = (color_idx * PIECE_TYPES + ptype) * SQUARES + sq
                features[idx] = 1.0
                col += 1

    # Encode hand features at offset 773
    hand_offset = PIECE_TYPES * COLORS * SQUARES + MODIFIERS
    if white_hand:
        for mechanic in white_hand:
            if isinstance(mechanic, dict):
                mechanic = mechanic.get('mechanic', '')
            idx = MECHANIC_INDEX.get(mechanic)
            if idx is not None:
                features[hand_offset + idx] = 1.0
    if black_hand:
        for mechanic in black_hand:
            if isinstance(mechanic, dict):
                mechanic = mechanic.get('mechanic', '')
            idx = MECHANIC_INDEX.get(mechanic)
            if idx is not None:
                features[hand_offset + 37 + idx] = 1.0

    return features


def load_go_training_data(path: str):
    with open(path, 'r') as f:
        data = json.load(f)
    positions = []
    for game in data.get('games', []):
        for pos in game.get('positions', []):
            fen = pos['fen']
            if ' ' not in fen:
                fen = fen + ' w'
            score = pos['score']
            white_hand = pos.get('whiteHand') or pos.get('white_hand')
            black_hand = pos.get('blackHand') or pos.get('black_hand')
            positions.append((fen, score, white_hand, black_hand))
    print(f"Loaded {len(positions)} positions from {len(data.get('games', []))} games in {path}")
    return positions


class ChessDataset(Dataset):
    def __init__(self, positions):
        self.positions = positions

    def __len__(self):
        return len(self.positions)

    def __getitem__(self, idx):
        fen, score, white_hand, black_hand = self.positions[idx]
        features = encode_fen(fen, white_hand, black_hand)
        target = score / 100.0
        return torch.tensor(features), torch.tensor([target], dtype=torch.float32)


def save_weights(model: NNUE, path: str):
    w1 = model.input_layer.weight.detach().numpy()
    b1 = model.input_layer.bias.detach().numpy()
    w2 = model.output_layer.weight.detach().numpy()
    b2 = model.output_layer.bias.detach().numpy()

    with open(path, 'wb') as f:
        f.write(struct.pack('<II', INPUT_SIZE, HIDDEN_SIZE))
        for val in w1.flatten():
            f.write(struct.pack('<f', val))
        for val in b1.flatten():
            f.write(struct.pack('<f', val))
        for val in w2.flatten():
            f.write(struct.pack('<f', val))
        for val in b2.flatten():
            f.write(struct.pack('<f', val))

    print(f"Weights saved to {path} ({INPUT_SIZE * HIDDEN_SIZE * 4 + HIDDEN_SIZE * 4 + HIDDEN_SIZE * 4 + 4 + 8} bytes)")


def load_weights(model: NNUE, path: str):
    with open(path, 'rb') as f:
        header = f.read(8)
        input_size = struct.unpack('<I', header[0:4])[0]
        hidden_size = struct.unpack('<I', header[4:8])[0]
        if input_size != INPUT_SIZE or hidden_size != HIDDEN_SIZE:
            print(f"Warning: weight dimensions ({input_size}x{hidden_size}) don't match expected ({INPUT_SIZE}x{HIDDEN_SIZE})")
            return
        w1 = np.frombuffer(f.read(INPUT_SIZE * HIDDEN_SIZE * 4), dtype=np.float32).reshape(HIDDEN_SIZE, INPUT_SIZE)
        b1 = np.frombuffer(f.read(HIDDEN_SIZE * 4), dtype=np.float32)
        w2 = np.frombuffer(f.read(HIDDEN_SIZE * 4), dtype=np.float32).reshape(1, HIDDEN_SIZE)
        b2 = np.frombuffer(f.read(4), dtype=np.float32)
        model.input_layer.weight.data = torch.tensor(w1)
        model.input_layer.bias.data = torch.tensor(b1)
        model.output_layer.weight.data = torch.tensor(w2)
        model.output_layer.bias.data = torch.tensor(b2)
        print(f"Loaded existing weights from {path}")


def generate_random_positions(num_positions: int = 50000):
    """Generate random chess positions evaluated with classical eval (no search needed)."""
    import chess
    import random
    positions = []
    board = chess.Board()
    pst = {
        'P': [0,0,0,0,0,0,0,0,50,50,50,50,50,50,50,50,10,10,20,30,30,20,10,10,5,5,10,25,25,10,5,5,0,0,0,20,20,0,0,0,5,-5,-10,0,0,-10,-5,5,5,10,10,-20,-20,10,10,5,0,0,0,0,0,0,0,0],
        'N': [-50,-40,-30,-30,-30,-30,-40,-50,-40,-20,0,0,0,0,-20,-40,-30,0,10,15,15,10,0,-30,-30,5,15,20,20,15,5,-30,-30,0,15,20,20,15,0,-30,-30,5,10,15,15,10,5,-30,-40,-20,0,5,5,0,-20,-40,-50,-40,-30,-30,-30,-30,-40,-50],
        'B': [-20,-10,-10,-10,-10,-10,-10,-20,-10,0,0,0,0,0,0,-10,-10,0,10,10,10,10,0,-10,-10,5,5,10,10,5,5,-10,-10,0,10,10,10,10,0,-10,-10,10,10,10,10,10,10,-10,-10,5,0,0,0,0,5,-10,-20,-10,-10,-10,-10,-10,-10,-20],
        'R': [0,0,0,0,0,0,0,0,5,10,10,10,10,10,10,5,-5,0,0,0,0,0,0,-5,-5,0,0,0,0,0,0,-5,-5,0,0,0,0,0,0,-5,-5,0,0,0,0,0,0,-5,-5,0,0,0,0,0,0,-5,0,0,0,5,5,0,0,0],
        'Q': [-20,-10,-10,-5,-5,-10,-10,-20,-10,0,0,0,0,0,0,-10,-10,0,5,5,5,5,0,-10,-5,0,5,5,5,5,0,-5,0,0,5,5,5,5,0,-5,-10,5,5,5,5,5,0,-10,-10,0,5,0,0,0,0,-10,-20,-10,-10,-5,-5,-10,-10,-20],
        'K': [-30,-40,-40,-50,-50,-40,-40,-30,-30,-40,-40,-50,-50,-40,-40,-30,-30,-40,-40,-50,-50,-40,-40,-30,-30,-40,-40,-50,-50,-40,-40,-30,-20,-30,-30,-40,-40,-30,-30,-20,-10,-20,-20,-20,-20,-20,-20,-10,20,20,0,0,0,0,20,20,20,30,10,0,0,10,30,20],
    }
    piece_vals = {'P': 100, 'N': 320, 'B': 330, 'R': 500, 'Q': 900, 'K': 0}
    
    for _ in tqdm(range(num_positions), desc="Generating random positions"):
        # Mix of random positions and game-like sequences
        for _ in range(random.randint(1, 20)):
            if board.is_game_over():
                board = chess.Board()
                break
            legal = list(board.legal_moves)
            if not legal:
                board = chess.Board()
                break
            board.push(random.choice(legal))
        
        fen = board.fen()
        
        # Classical eval: material + piece-square tables
        score = 0
        for sq in chess.SQUARES:
            p = board.piece_at(sq)
            if p is None:
                continue
            val = piece_vals.get(p.symbol().upper(), 0)
            pst_idx = sq if p.color == chess.WHITE else 63 - sq
            bonus = pst.get(p.symbol().upper(), [0]*64)[pst_idx]
            total = val + bonus
            score += total if p.color == chess.WHITE else -total
        
        # Flip to white's perspective for go engine compatibility
        if board.turn == chess.BLACK:
            score = -score
        
        positions.append((fen, score, None, None))
    
    return positions


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", type=str, default=None)
    parser.add_argument("--generate", type=int, default=0, help="Generate N random positions (no Go data needed)")
    parser.add_argument("--epochs", type=int, default=100)
    parser.add_argument("--lr", type=float, default=0.003)
    parser.add_argument("--batch-size", type=int, default=256)
    parser.add_argument("--output", type=str, default="services/realtime/nnue_weights.bin")
    parser.add_argument("--load", type=str, default=None)
    parser.add_argument("--patience", type=int, default=15)
    args = parser.parse_args()

    if not args.data:
        print("Error: --data is required")
        exit(1)

    positions = load_go_training_data(args.data)

    scores = np.array([s for _, s, _, _ in positions])
    print(f"  {len(positions)} total positions")
    print(f"  Score range: {scores.min():.0f} to {scores.max():.0f}")
    print(f"  Mean: {scores.mean():.1f}, Std: {scores.std():.1f}")

    np.random.seed(42)
    torch.manual_seed(42)
    idxs = np.random.permutation(len(positions))
    split = int(0.9 * len(positions))
    train_pos = [positions[i] for i in idxs[:split]]
    val_pos = [positions[i] for i in idxs[split:]]

    train_ds = ChessDataset(train_pos)
    val_ds = ChessDataset(val_pos)
    train_loader = DataLoader(train_ds, batch_size=args.batch_size, shuffle=True, num_workers=0)
    val_loader = DataLoader(val_ds, batch_size=args.batch_size, shuffle=False, num_workers=0)

    model = NNUE()
    if args.load:
        load_weights(model, args.load)

    criterion = nn.HuberLoss(delta=1.0)
    optimizer = optim.AdamW(model.parameters(), lr=args.lr, weight_decay=1e-5)
    scheduler = optim.lr_scheduler.ReduceLROnPlateau(optimizer, mode='min', factor=0.5, patience=5, min_lr=1e-6)

    best_val_loss = float('inf')
    patience_counter = 0

    for epoch in range(args.epochs):
        model.train()
        train_loss = 0.0
        for batch_x, batch_y in tqdm(train_loader, desc=f"Epoch {epoch+1}/{args.epochs}", leave=False):
            optimizer.zero_grad()
            pred = model(batch_x)
            loss = criterion(pred, batch_y)
            loss.backward()
            torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
            optimizer.step()
            train_loss += loss.item()
        avg_train = train_loss / len(train_loader)

        model.eval()
        val_loss = 0.0
        with torch.no_grad():
            for batch_x, batch_y in val_loader:
                pred = model(batch_x)
                loss = criterion(pred, batch_y)
                val_loss += loss.item()
        avg_val = val_loss / len(val_loader)

        scheduler.step(avg_val)

        print(f"  Epoch {epoch+1}: train={avg_train:.4f} val={avg_val:.4f} "
              f"lr={optimizer.param_groups[0]['lr']:.6f}")

        if avg_val < best_val_loss:
            best_val_loss = avg_val
            patience_counter = 0
        else:
            patience_counter += 1
            if patience_counter >= args.patience:
                print(f"  Early stopping at epoch {epoch+1}")
                break

    save_weights(model, args.output)
    print(f"Best val loss: {best_val_loss:.4f}")
    print("Done!")
