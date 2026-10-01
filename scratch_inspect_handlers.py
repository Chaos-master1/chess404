import sys
import re

sys.stdout.reconfigure(encoding='utf-8')

with open('f:/chess404/apps/web/src/hooks/useCardInteraction.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    lines = f.readlines()

for i, l in enumerate(lines):
    m = re.match(r"^\s*case '([a-z_]+)':", l)
    if m:
        mech = m.group(1)
        print(f"\n{'='*40}\nLINE {i+1}: case '{mech}':")
        # print next 25 lines
        for j in range(i, min(len(lines), i + 25)):
            print(f"  {lines[j]}", end='')
