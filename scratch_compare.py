import json
import re
import sys

sys.stdout.reconfigure(encoding='utf-8')

with open('F:/chess2/App.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c2_code = f.read()

with open('f:/chess404/packages/game-core/src/cards.json', 'r', encoding='utf-8', errors='ignore') as f:
    c404_cards = json.load(f)

# Extract CARD_POOL lines from chess2
pool_start = c2_code.find('const CARD_POOL')
pool_end = c2_code.find('];', pool_start)
pool_block = c2_code[pool_start:pool_end]

c2_cards = []
for line in pool_block.splitlines():
    line = line.strip()
    if not line.startswith('{') or 'mechanic:' not in line:
        continue
    # Extract fields
    name = re.search(r"name:\s*['\"]([^'\"]+)['\"]", line)
    mech = re.search(r"mechanic:\s*['\"]([^'\"]+)['\"]", line)
    typ = re.search(r"type:\s*['\"]([^'\"]+)['\"]", line)
    rarity = re.search(r"rarity:\s*['\"]([^'\"]+)['\"]", line)
    icon = re.search(r"icon:\s*['\"]([^'\"]+)['\"]", line)
    desc = re.search(r"desc:\s*['\"]([^'\"]+)['\"]", line)
    if mech and name:
        c2_cards.append({
            'name': name.group(1),
            'mechanic': mech.group(1),
            'type': typ.group(1) if typ else '',
            'rarity': rarity.group(1) if rarity else '',
            'icon': icon.group(1) if icon else '',
            'desc': desc.group(1) if desc else ''
        })

print(f"Parsed {len(c2_cards)} cards from chess2:\n")
for c in c2_cards:
    print(f"  {c['mechanic']:18} | {c['name']:20} | {c['rarity']:10} | {c['icon']:2} | {c['desc']}")

print("\n" + "="*80)
print(f"chess404 cards ({len(c404_cards)} cards):\n")
c404_by_mech = {}
for c in c404_cards:
    m = c.get('mechanic') or c.get('id')
    c404_by_mech[m] = c

for m, c in c404_by_mech.items():
    print(f"  {m:18} | {c.get('name',''):20} | {c.get('rarity',''):10} | {c.get('icon',''):2} | {c.get('description','')}")

print("\n" + "="*80)
print("METADATA & RARITY & NAMING COMPARISON:")
for c2 in c2_cards:
    m = c2['mechanic']
    if m not in c404_by_mech:
        print(f"[MISSING IN 404] {m} ({c2['name']})")
    else:
        c4 = c404_by_mech[m]
        diffs = []
        if c2['name'] != c4.get('name'):
            diffs.append(f"Name: '{c2['name']}' -> '{c4.get('name')}'")
        if c2['rarity'] != c4.get('rarity'):
            diffs.append(f"Rarity: '{c2['rarity']}' -> '{c4.get('rarity')}'")
        if c2['icon'] != c4.get('icon'):
            diffs.append(f"Icon: '{c2['icon']}' -> '{c4.get('icon')}'")
        if diffs:
            print(f"[DIFF: {m:18}] " + " | ".join(diffs))

for m, c4 in c404_by_mech.items():
    if not any(c['mechanic'] == m for c in c2_cards):
        print(f"[ADDED IN 404] {m} ({c4.get('name')})")
