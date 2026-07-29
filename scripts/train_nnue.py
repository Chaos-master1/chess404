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
HAND_FEATURES = 74
INPUT_SIZE = PIECE_TYPES * COLORS * SQUARES + MODIFIERS + HAND_FEATURES
HIDDEN_SIZE = 1024
HIDDEN_SIZE2 = 1024

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
        self.l1 = nn.Linear(INPUT_SIZE, HIDDEN_SIZE)
        self.l2 = nn.Linear(HIDDEN_SIZE, HIDDEN_SIZE2)
        self.l3 = nn.Linear(HIDDEN_SIZE2, 1)

    def forward(self, x):
        h = self.l1(x)
        h = torch.clamp(h, min=0)
        h = self.l2(h)
        h = torch.clamp(h, min=0)
        out = self.l3(h)
        return out


pieces_map = {
    'P': (0, 0), 'N': (1, 0), 'B': (2, 0), 'R': (3, 0), 'Q': (4, 0), 'K': (5, 0),
    'p': (0, 1), 'n': (1, 1), 'b': (2, 1), 'r': (3, 1), 'q': (4, 1), 'k': (5, 1),
}


def encode_fen(board_fen, white_hand=None, black_hand=None):
    # A FEN's rank segments run top-to-bottom (rows[0] is rank 8), but the Go
    # engine's board -- and its own NNUE encoder in nnue.go's encodeBoard --
    # index board[0] as rank 1, i.e. a1 is square 0. This must convert between
    # the two, or every square index computed here is the vertical mirror of
    # what the Go engine will query the resulting weights on: `board_row =
    # 7 - r` is the same conversion Go's own FEN parser uses at
    # perft.go's `boardRow := 7 - fenRow`.
    #
    # See TestNNUEBoardEncodingContractWithTrainer in nnue_test.go, which pins
    # the index this formula must produce for a fixed reference square so the
    # two sides can be checked against each other by inspection.
    parts = board_fen.strip().split()
    fen_part = parts[0]
    rows = fen_part.split('/')
    features = np.zeros(INPUT_SIZE, dtype=np.float32)
    for r, row_str in enumerate(rows):
        board_row = 7 - r
        col = 0
        for ch in row_str:
            if ch.isdigit():
                col += int(ch)
                continue
            if ch in pieces_map:
                ptype, color_idx = pieces_map[ch]
                sq = board_row * 8 + col
                idx = (color_idx * PIECE_TYPES + ptype) * SQUARES + sq
                features[idx] = 1.0
                col += 1

    # The 5 modifier input features (lava / bomb / fortress / fog / blackhole
    # presence -- see nnue.go's encodeModifiers) are intentionally left at 0
    # here, not set from `parts`. This is NOT the bug it looks like at first
    # glance: a FEN string has nowhere to encode these -- they are Chess404
    # card effects, not part of standard chess notation -- so there is no
    # information in `board_fen` this function COULD read them from. The 0.1x
    # leaky-ReLU / plain-ReLU mismatch and the board orientation flip (fixed
    # above) were both self-contained bugs fixable by aligning two encoders
    # that already receive the same information. This one is structural: the
    # Go self-play pipeline that generates this trainer's input data
    # (selfplay.go) deals hands but never actually plays a card, so no
    # self-play position has ever had lava/fortress/bombs/fog/a blackhole on
    # the board, and there is no real signal here to fit even if this
    # function were wired up to read it. Fixing self-play to play cards is
    # tracked separately (Phase 4 of the engine rebuild plan, "self-play that
    # actually plays cards") -- do that first, thread the resulting per-position
    # modifier state through load_go_training_data, and only then set these
    # features here.
    mod_offset = PIECE_TYPES * COLORS * SQUARES

    hand_offset = mod_offset + MODIFIERS
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


def load_go_training_data(path):
    with open(path, 'r') as f:
        data = json.load(f)
    positions = []
    for game in data.get('games', []):
        for pos in game.get('positions', []):
            fen = pos['fen']
            if ' ' not in fen:
                fen = fen + ' w'
            score = pos.get('score', 0)
            outcome = pos.get('outcome', 0)
            white_hand = pos.get('whiteHand') or pos.get('white_hand')
            black_hand = pos.get('blackHand') or pos.get('black_hand')
            positions.append((fen, score, outcome, white_hand, black_hand))
    print(f"Loaded {len(positions)} positions from {len(data.get('games', []))} games in {path}")
    return positions


def compute_td_lambda(positions, lam=0.7):
    n = len(positions)
    targets = []
    if n == 0:
        return targets
    values = np.array([s / 100.0 for _, s, _, _, _ in positions])
    final_outcome = positions[-1][2]
    values = np.append(values, final_outcome)
    target = float(values[-1])
    for i in range(n - 1, -1, -1):
        td_err = values[i + 1] - values[i]
        target = values[i] + lam * td_err
        targets.append(float(target))
    targets.reverse()
    for i in range(n):
        positions[i] = (positions[i][0], targets[i], positions[i][2],
                        positions[i][3], positions[i][4])
    return positions


class ChessDataset(Dataset):
    def __init__(self, positions):
        self.positions = positions

    def __len__(self):
        return len(self.positions)

    def __getitem__(self, idx):
        entry = self.positions[idx]
        fen = entry[0]
        score = entry[1]
        white_hand = entry[3]
        black_hand = entry[4]
        features = encode_fen(fen, white_hand, black_hand)
        target = score / 100.0
        return torch.tensor(features), torch.tensor([target], dtype=torch.float32)


def save_weights(model, path):
    w1 = model.l1.weight.detach().numpy()
    b1 = model.l1.bias.detach().numpy()
    w2 = model.l2.weight.detach().numpy()
    b2 = model.l2.bias.detach().numpy()
    w3 = model.l3.weight.detach().numpy()
    b3 = model.l3.bias.detach().numpy()

    with open(path, 'wb') as f:
        f.write(struct.pack('<III', INPUT_SIZE, HIDDEN_SIZE, HIDDEN_SIZE2))
        for val in w1.flatten():
            f.write(struct.pack('<f', val))
        for val in b1.flatten():
            f.write(struct.pack('<f', val))
        for val in w2.flatten():
            f.write(struct.pack('<f', val))
        for val in b2.flatten():
            f.write(struct.pack('<f', val))
        for val in w3.flatten():
            f.write(struct.pack('<f', val))
        for val in b3.flatten():
            f.write(struct.pack('<f', val))

    print(f"Weights saved to {path} ({INPUT_SIZE * HIDDEN_SIZE * 4 + HIDDEN_SIZE * 4 + HIDDEN_SIZE * HIDDEN_SIZE2 * 4 + HIDDEN_SIZE2 * 4 + HIDDEN_SIZE2 * 4 + 4 + 12} bytes)")


def load_weights(model, path):
    with open(path, 'rb') as f:
        header = f.read(12)
        input_size = struct.unpack('<I', header[0:4])[0]
        hidden_size = struct.unpack('<I', header[4:8])[0]
        hidden_size2 = struct.unpack('<I', header[8:12])[0]
        if input_size != INPUT_SIZE or hidden_size != HIDDEN_SIZE or hidden_size2 != HIDDEN_SIZE2:
            print(f"Warning: weight dims ({input_size}x{hidden_size}x{hidden_size2}) != expected ({INPUT_SIZE}x{HIDDEN_SIZE}x{HIDDEN_SIZE2})")
            return
        w1 = np.frombuffer(f.read(INPUT_SIZE * HIDDEN_SIZE * 4), dtype=np.float32).reshape(HIDDEN_SIZE, INPUT_SIZE)
        b1 = np.frombuffer(f.read(HIDDEN_SIZE * 4), dtype=np.float32)
        w2 = np.frombuffer(f.read(HIDDEN_SIZE * HIDDEN_SIZE2 * 4), dtype=np.float32).reshape(HIDDEN_SIZE2, HIDDEN_SIZE)
        b2 = np.frombuffer(f.read(HIDDEN_SIZE2 * 4), dtype=np.float32)
        w3 = np.frombuffer(f.read(HIDDEN_SIZE2 * 4), dtype=np.float32).reshape(1, HIDDEN_SIZE2)
        b3 = np.frombuffer(f.read(4), dtype=np.float32)
        model.l1.weight.data = torch.tensor(w1)
        model.l1.bias.data = torch.tensor(b1)
        model.l2.weight.data = torch.tensor(w2)
        model.l2.bias.data = torch.tensor(b2)
        model.l3.weight.data = torch.tensor(w3)
        model.l3.bias.data = torch.tensor(b3)
        print(f"Loaded existing weights from {path}")


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", type=str, required=True)
    parser.add_argument("--epochs", type=int, default=100)
    parser.add_argument("--lr", type=float, default=0.001)
    parser.add_argument("--batch-size", type=int, default=256)
    parser.add_argument("--output", type=str, default="services/realtime/nnue_weights.bin")
    parser.add_argument("--load", type=str, default=None)
    parser.add_argument("--patience", type=int, default=15)
    parser.add_argument("--td-lambda", type=float, default=0.0, help="TD(λ) if > 0")
    args = parser.parse_args()

    positions = load_go_training_data(args.data)
    if args.td_lambda > 0:
        by_game = {}
        for p in positions:
            gid = id(p)
        positions = compute_td_lambda(positions, args.td_lambda)

    scores = np.array([s for _, s, _, _, _ in positions])
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
