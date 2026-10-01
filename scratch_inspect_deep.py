import sys
import re

sys.stdout.reconfigure(encoding='utf-8')

with open('F:/chess2/App.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c2 = f.read()

# Let's extract all 36 mechanics and their descriptions and handlers in chess2
# We already know the 36 mechanics:
mechs = [
    'badsniper', 'demote', 'gambler', 'halffuse', 'swapme', 'jump',
    'smallsacrifice', 'freeze', 'promote', 'shield', 'fogvillage',
    'fullfusion', 'swapus', 'swaphim', 'doublemove_diff', 'doublemove_same',
    'demotehim', 'promotehim', 'fakepiece', 'teleport', 'lavaground',
    'radar', 'mirror', 'sniper', 'fortress', 'clone', 'borrow',
    'parasite', 'blackhole', 'bigsacrifice', 'undo', 'reverse',
    'unabomber', 'mindcontrol', 'joker', 'cheater'
]

print(f"Total mechanics to check: {len(mechs)}")

# In chess2, how are turn ticks handled for bombs, lava, shield, borrow, freeze, etc.?
# Let's find doMove or processBombs or turn transition in chess2!
tick_matches = re.findall(r'(processBombs|handleLavaLanding|bombPieces|lavaSquares|frozen|shielded|borrowed)', c2)
print("State references in chess2:", set(tick_matches))
