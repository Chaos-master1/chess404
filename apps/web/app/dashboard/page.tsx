'use client';

import { useState } from 'react';
import { useDashboard } from '../../src/components/Dashboard/useDashboard';
import StatusBar from '../../src/components/Dashboard/StatusBar';
import EvalChart from '../../src/components/Dashboard/EvalChart';
import SearchTree from '../../src/components/Dashboard/SearchTree';
import EngineStream from '../../src/components/Dashboard/EngineStream';

export default function DashboardPage() {
  const [wsUrl, setWsUrl] = useState('ws://localhost:8765/ws');
  const [inputUrl, setInputUrl] = useState(wsUrl);

  const {
    connected,
    depthHistory,
    evalHistory,
    nodes,
    nps,
    bestMove,
    lastScore,
    log,
    currentLine,
    numDepths,
  } = useDashboard(wsUrl);

  return (
    <div className="min-h-screen bg-slate-950 text-slate-200 p-4 font-mono">
      <div className="max-w-6xl mx-auto">
        <header className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-lg font-bold tracking-tight">404 Chess Engine</h1>
            <p className="text-xs text-slate-500">Live Thinking Dashboard</p>
          </div>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setWsUrl(inputUrl);
            }}
            className="flex items-center gap-2"
          >
            <input
              type="text"
              value={inputUrl}
              onChange={(e) => setInputUrl(e.target.value)}
              className="bg-slate-900 border border-slate-700 rounded px-2 py-1 text-xs w-56 focus:outline-none focus:border-slate-500"
              placeholder="ws://localhost:8765/ws"
            />
            <button
              type="submit"
              className="bg-slate-700 hover:bg-slate-600 text-xs px-3 py-1 rounded transition-colors"
            >
              Connect
            </button>
          </form>
        </header>

        <div className="space-y-3">
          <StatusBar
            connected={connected}
            nodes={nodes}
            nps={nps}
            bestMove={bestMove}
            lastScore={lastScore}
            numDepths={numDepths}
            currentLine={currentLine}
          />

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
            <EvalChart history={evalHistory} />
            <SearchTree depths={depthHistory} />
          </div>

          <EngineStream lines={log} />
        </div>
      </div>
    </div>
  );
}
