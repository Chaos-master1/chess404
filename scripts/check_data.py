import json
with open('C:/Users/Expert Gaming/Desktop/chess404/services/realtime/training_data.json') as f:
    data = json.load(f)
positions = [p for g in data.get('games', []) for p in g.get('positions', [])]
scores = [p['score'] for p in positions]
print(f'Games: {len(data.get("games", []))}')
print(f'Positions: {len(positions)}')
print(f'Score range: {min(scores)} to {max(scores)}')
print(f'Mean score: {sum(scores)/len(scores):.1f}')
print(f'Sample scores (first 10): {scores[:10]}')
print(f'Sample scores (last 10): {scores[-10:]}')
has_extreme = [s for s in scores if abs(s) > 10000]
print(f'Extreme scores (|s|>10000): {len(has_extreme)}')
if has_extreme:
    print(f'  Examples: {has_extreme[:5]}')
