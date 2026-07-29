import json
with open('training_data.json') as f:
    d = json.load(f)
for g in d['games']:
    print('Positions:', len(g['positions']))
    for p in g['positions'][:3]:
        print('  FEN:', p['fen'][:40], '... Score:', p['score'])
    print('  ...')
    for p in g['positions'][-3:]:
        print('  FEN:', p['fen'][:40], '... Score:', p['score'])
scores = [p['score'] for g in d['games'] for p in g['positions']]
good = [s for s in scores if -100000 < s < 100000]
print('Total positions:', len(scores))
print('Scores in [-100000, 100000]:', len(good), '/', len(scores))
if good:
    print('Good score range:', min(good), 'to', max(good))
print('Min:', min(scores), 'Max:', max(scores))
