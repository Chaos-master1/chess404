'use client';

import { useRef, useEffect } from 'react';

interface Props {
  lines: string[];
}

export default function EngineStream({ lines }: Props) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [lines.length]);

  return (
    <div className="rounded-lg border border-slate-700 bg-slate-950 p-3">
      <div className="mb-2 text-xs font-medium text-slate-400 uppercase tracking-wider">Event Log</div>
      <div className="h-40 overflow-y-auto font-mono text-xs space-y-0.5">
        {lines.length === 0 && (
          <span className="text-slate-600">No events yet...</span>
        )}
        {lines.map((line, i) => (
          <div key={i} className="text-slate-400">
            {line}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
