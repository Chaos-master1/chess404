import sys
import re

sys.stdout.reconfigure(encoding='utf-8')

with open('F:/chess2/App.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c2 = f.read()

with open('f:/chess404/services/realtime/internal/match/cards_target_select.go', 'r', encoding='utf-8', errors='ignore') as f:
    go_target = f.read()

with open('f:/chess404/services/realtime/internal/match/cards_mechanics.go', 'r', encoding='utf-8', errors='ignore') as f:
    go_mech = f.read()

with open('f:/chess404/services/realtime/internal/match/cards_play.go', 'r', encoding='utf-8', errors='ignore') as f:
    go_play = f.read()

# Let's inspect each card's rules in Go
cards = [
    'badsniper', 'demote', 'gambler', 'halffuse', 'swapme', 'jump',
    'smallsacrifice', 'freeze', 'promote', 'shield', 'fog_village',
    'fullfusion', 'swapus', 'swaphim', 'doublemove_diff', 'doublemove_same',
    'demotehim', 'promotehim', 'fakepiece', 'teleport', 'lavaground',
    'radar', 'mirror', 'sniper', 'fortress', 'clone', 'borrow',
    'parasite', 'blackhole', 'bigsacrifice', 'undo', 'reverse',
    'unabomber', 'mindcontrol', 'joker', 'cheater'
]

print("=== CHECKING GO IMPLEMENTATIONS ===")
for card in cards:
    found = []
    if f'case "{card}":' in go_target or f'case "{card}",' in go_target:
        found.append("target_select")
    if f'case "{card}":' in go_play or f'case "{card}",' in go_play:
        found.append("play")
    if f'"{card}"' in go_mech:
        found.append("mechanics")
    print(f"{card:18}: {', '.join(found) if found else 'NOT FOUND'}")
