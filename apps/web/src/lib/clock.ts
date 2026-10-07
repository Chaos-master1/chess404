// Shared time-control formatting for surfaces that display a game's clock
// config (queue tickets, player bars). Matches the queue/challenge pickers'
// "base+increment" notation.
export function clockLabel(seconds?: number, increment?: number): string {
  if (!seconds || seconds <= 0) {
    return '10+0';
  }
  return `${seconds}+${increment ?? 0}`;
}
