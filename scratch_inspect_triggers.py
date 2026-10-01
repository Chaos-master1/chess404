import sys
import re

sys.stdout.reconfigure(encoding='utf-8')

with open('f:/chess404/apps/web/src/hooks/useCardInteraction.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c404 = f.read()

with open('F:/chess2/App.tsx', 'r', encoding='utf-8', errors='ignore') as f:
    c2 = f.read()

# Let's inspect how animations are triggered in chess404
# Look for triggerSwapAnim, setTransformAnim, setSniperAnim, etc.
print("--- Animation triggers in useCardInteraction.tsx ---")
for m in re.finditer(r'(set\w+Anim|trigger\w+|spawn\w+)\([^)]*\)', c404):
    print(m.group(0))

print("\n--- Animation triggers in F:/chess2/App.tsx ---")
for m in re.finditer(r'(trigger\w+|spawn\w+)\([^)]*\)', c2):
    print(m.group(0))
