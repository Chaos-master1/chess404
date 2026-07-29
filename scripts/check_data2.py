import json
import numpy as np

with open('C:/Users/Expert Gaming/Desktop/chess404/services/realtime/training_raw.json') as f:
    data = json.load(f)

scores = []
for game in data.get('games', []):
    for pos in game.get('positions', []):
        scores.append(pos['score'])

s = np.array(scores)
print(f"Positions: {len(s)}")
print(f"Range: {s.min():.0f} to {s.max():.0f}")
print(f"Mean: {s.mean():.1f}, Std: {s.std():.1f}")
print(f"Median: {np.median(s):.1f}")
print(f"Zeros: {(s==0).sum()}")
print(f"First 20: {s[:20].tolist()}")
