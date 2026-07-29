'use client';

import type { DepthInfo } from './useDashboard';

interface Props {
  depths: DepthInfo[];
}

export default function SearchTree({ depths }: Props) {
  if (depths.length === 0) {
    return (
      <div className="rounded-lg border border-slate-700 bg-slate-900 p-3 h-64 flex items-center justify-center">
        <span className="text-slate-500 text-sm">Waiting for search...</span>
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-slate-700 bg-slate-900 p-3">
      <div className="mb-2 text-xs font-medium text-slate-400 uppercase tracking-wider">Search Tree</div>
      <div className="space-y-1.5 max-h-64 overflow-y-auto">
        {depths.map((d, i) => {
          const evalStr = (d.score / 100).toFixed(2);
          const evalColor = d.score >= 0 ? 'text-green-400' : 'text-red-400';
          const isLast = i === depths.length - 1;

          return (
            <div
              key={i}
              className={`flex items-center gap-3 px-2 py-1.5 rounded text-xs font-mono transition-colors ${
                isLast ? 'bg-slate-700/60 ring-1 ring-slate-500' : 'hover:bg-slate-800/50'
              }`}
            >
              <span className="w-12 text-slate-500 shrink-0">d{d.depth}</span>
              <span className={`w-16 shrink-0 font-semibold ${evalColor}`}>{evalStr}</span>
              <span className="w-24 truncate shrink-0 text-slate-300">{d.move}</span>
              <div className="flex-1 min-w-0 flex gap-1 overflow-hidden">
                {(d.pv ?? []).slice(0, 6).map((m, j) => (
                  <span key={j} className="text-slate-500 truncate">
                    {j > 0 && <span className="mx-0.5 text-slate-700">·</span>}
                    {m}
                  </span>
                ))}
              </div>
              <span className="w-24 text-right text-slate-500 shrink-0">
                {d.nodes.toLocaleString()} <span className="text-slate-600">n</span>
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
