'use client';

interface Props {
  connected: boolean;
  nodes: number;
  nps: number;
  bestMove: string;
  lastScore: number;
  numDepths: number;
  currentLine: string;
}

export default function StatusBar({ connected, nodes, nps, bestMove, lastScore, numDepths, currentLine }: Props) {
  return (
    <div className="rounded-lg border border-slate-700 bg-slate-900 p-3">
      <div className="flex flex-wrap items-center gap-4 text-xs font-mono">
        <div className="flex items-center gap-1.5">
          <span className={`inline-block w-2 h-2 rounded-full ${connected ? 'bg-green-400' : 'bg-red-500'}`} />
          <span className="text-slate-400">{connected ? 'Connected' : 'Disconnected'}</span>
        </div>
        <div className="text-slate-500">
          Nodes: <span className="text-slate-300">{nodes.toLocaleString()}</span>
        </div>
        <div className="text-slate-500">
          NPS: <span className="text-slate-300">{nps.toLocaleString()}</span>
        </div>
        <div className="text-slate-500">
          Depth: <span className="text-slate-300">{numDepths}</span>
        </div>
        {bestMove && (
          <div className="text-slate-500">
            Best: <span className="text-amber-400 font-semibold">{bestMove}</span>
            <span className={lastScore >= 0 ? 'text-green-400 ml-1' : 'text-red-400 ml-1'}>
              {(lastScore / 100).toFixed(2)}
            </span>
          </div>
        )}
      </div>
      {currentLine && (
        <div className="mt-2 text-xs text-slate-500 font-mono truncate">{currentLine}</div>
      )}
    </div>
  );
}
