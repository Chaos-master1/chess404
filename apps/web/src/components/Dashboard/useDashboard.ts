'use client';

import { useEffect, useRef, useState, useCallback } from 'react';

export interface SearchEvent {
  type: string;
  depth?: number;
  score?: number;
  move?: string;
  nodes?: number;
  nps?: number;
  alpha?: number;
  beta?: number;
  reason?: string;
  pv?: string[];
  ts: number;
}

export interface DepthInfo {
  depth: number;
  score: number;
  move: string;
  nodes: number;
  nps: number;
  pv: string[];
}

export interface EvalPoint {
  time: number;
  score: number;
  nodes: number;
}

export function useDashboard(url: string) {
  const [connected, setConnected] = useState(false);
  const [depthHistory, setDepthHistory] = useState<DepthInfo[]>([]);
  const [evalHistory, setEvalHistory] = useState<EvalPoint[]>([]);
  const [nodes, setNodes] = useState(0);
  const [nps, setNps] = useState(0);
  const [bestMove, setBestMove] = useState('');
  const [lastScore, setLastScore] = useState(0);
  const [log, setLog] = useState<string[]>([]);
  const [currentLine, setCurrentLine] = useState('');
  const wsRef = useRef<WebSocket | null>(null);
  const logRef = useRef<string[]>([]);

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) return;
    try {
      const ws = new WebSocket(url);
      ws.onopen = () => setConnected(true);
      ws.onclose = () => {
        setConnected(false);
        setTimeout(connect, 2000);
      };
      ws.onerror = () => ws.close();
      ws.onmessage = (msg) => {
        try {
          const ev: SearchEvent = JSON.parse(msg.data);

          switch (ev.type) {
            case 'search_start':
              setCurrentLine(`Depth ${ev.depth} — ${ev.nodes?.toLocaleString()} nodes`);
              break;

            case 'depth_done':
              setDepthHistory(prev => [...prev, {
                depth: ev.depth ?? 0,
                score: ev.score ?? 0,
                move: ev.move ?? '',
                nodes: ev.nodes ?? 0,
                nps: ev.nps ?? 0,
                pv: ev.pv ?? [],
              }]);
              setNodes(ev.nodes ?? 0);
              setNps(ev.nps ?? 0);
              setLastScore(ev.score ?? 0);
              setEvalHistory(prev => [...prev, {
                time: Date.now(),
                score: ev.score ?? 0,
                nodes: ev.nodes ?? 0,
              }]);
              setCurrentLine(`Depth ${ev.depth}: ${ev.move} = ${(ev.score ?? 0) / 100} (${ev.nodes?.toLocaleString()} nodes, ${ev.nps?.toLocaleString()} nps)`);
              break;

            case 'best_move':
              setBestMove(ev.move ?? '');
              setLastScore(ev.score ?? 0);
              setCurrentLine(`Best move: ${ev.move} = ${(ev.score ?? 0) / 100}`);
              break;

            case 'info':
              addLog(ev.reason ?? '');
              break;
          }
        } catch { }
      };
      wsRef.current = ws;
    } catch { }
  }, [url]);

  const addLog = (msg: string) => {
    logRef.current = [...logRef.current.slice(-99), `[${new Date().toLocaleTimeString()}] ${msg}`];
    setLog(logRef.current);
  };

  useEffect(() => {
    connect();
    return () => wsRef.current?.close();
  }, [connect]);

  return {
    connected,
    depthHistory,
    evalHistory,
    nodes,
    nps,
    bestMove,
    lastScore,
    log,
    currentLine,
    numDepths: depthHistory.length,
  };
}
