import sys
import json
import re

sys.stdout.reconfigure(encoding='utf-8')

# Read chess2 CARD_POOL
with open('F:/chess2/App.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c2 = f.read()

# Read chess404 cards.json
with open('f:/chess404/packages/game-core/src/cards.json', 'r', encoding='utf-8', errors='ignore') as f:
    c404 = json.load(f)

c404_by_id = {c['id']: c for c in c404}

# Extract 36 cards from chess2
pool_start = c2.find('const CARD_POOL')
pool_end = c2.find('];', pool_start)
pool_block = c2[pool_start:pool_end]

cards2 = []
for line in pool_block.splitlines():
    line = line.strip()
    if not line.startswith('{') or 'mechanic:' not in line:
        continue
    name = re.search(r"name:\s*['\"]([^'\"]+)['\"]", line).group(1)
    mech = re.search(r"mechanic:\s*['\"]([^'\"]+)['\"]", line).group(1)
    typ = re.search(r"type:\s*['\"]([^'\"]+)['\"]", line).group(1)
    rarity = re.search(r"rarity:\s*['\"]([^'\"]+)['\"]", line).group(1)
    icon = re.search(r"icon:\s*['\"]([^'\"]+)['\"]", line).group(1)
    desc = re.search(r"desc:\s*['\"]([^'\"]+)['\"]", line).group(1)
    cards2.append({
        'name': name, 'mechanic': mech, 'type': typ,
        'rarity': rarity, 'icon': icon, 'desc': desc
    })

print(f"Total chess2 cards: {len(cards2)}")

# Let's inspect each card's visual in chess2 BoardCanvas
# Look for occurrences of mechanic in BoardCanvas of chess2
canvas_start = c2.find('const BoardCanvas')
canvas_end = c2.find('// Mouse-based drag handling', canvas_start)
canvas_code = c2[canvas_start:canvas_end]

print(f"BoardCanvas size in chess2: {len(canvas_code)} chars")

for c in cards2:
    m = c['mechanic']
    # Check if mechanic or its effect is explicitly drawn in BoardCanvas
    mentions = []
    if m in canvas_code:
        mentions.append('direct_mention')
    if m == 'unabomber' and 'bomb' in canvas_code:
        mentions.append('bomb_rendering')
    if m == 'lavaground' and 'lava' in canvas_code:
        mentions.append('lava_rendering')
    if m in ['swapme', 'swapus', 'swaphim'] and 'swap' in canvas_code:
        mentions.append('swap_arc')
    if m == 'freeze' and 'frozen' in canvas_code:
        mentions.append('frozen_visual')
    if m == 'shield' and 'shielded' in canvas_code:
        mentions.append('shield_visual')
    if m in ['doublemove_diff', 'doublemove_same'] and 'doubleMove' in canvas_code:
        mentions.append('doublemove_visual')
    print(f"{m:18} | {c['name']:20} | visuals: {mentions}")
