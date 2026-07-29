'use client';

import { useRef, useEffect } from 'react';
import type { EvalPoint } from './useDashboard';

interface Props {
  history: EvalPoint[];
}

export default function EvalChart({ history }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || history.length === 0) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const w = canvas.clientWidth * dpr;
    const h = canvas.clientHeight * dpr;
    canvas.width = w;
    canvas.height = h;
    ctx.scale(dpr, dpr);
    const cw = canvas.clientWidth;
    const ch = canvas.clientHeight;

    ctx.clearRect(0, 0, cw, ch);

    const scores = history.map(p => p.score);
    const minScore = Math.min(...scores, -100);
    const maxScore = Math.max(...scores, 100);
    const range = Math.max(maxScore - minScore, 200);

    const toY = (s: number) => ch - ((s - minScore) / range) * (ch - 20) - 10;
    const padding = 5;
    const stepX = (cw - padding * 2) / Math.max(history.length - 1, 1);

    // Grid lines
    ctx.strokeStyle = '#1e293b';
    ctx.lineWidth = 1;
    for (let i = 0; i < 4; i++) {
      const y = (ch / 4) * i;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(cw, y);
      ctx.stroke();
    }

    // Area fill
    ctx.beginPath();
    ctx.moveTo(padding, toY(history[0].score));
    history.forEach((p, i) => ctx.lineTo(padding + i * stepX, toY(p.score)));
    ctx.lineTo(padding + (history.length - 1) * stepX, ch);
    ctx.lineTo(padding, ch);
    ctx.closePath();
    const gradient = ctx.createLinearGradient(0, 0, 0, ch);
    const lastScore = history[history.length - 1].score;
    if (lastScore >= 0) {
      gradient.addColorStop(0, 'rgba(34, 197, 94, 0.3)');
      gradient.addColorStop(1, 'rgba(34, 197, 94, 0.02)');
    } else {
      gradient.addColorStop(0, 'rgba(239, 68, 68, 0.3)');
      gradient.addColorStop(1, 'rgba(239, 68, 68, 0.02)');
    }
    ctx.fillStyle = gradient;
    ctx.fill();

    // Line
    ctx.beginPath();
    ctx.moveTo(padding, toY(history[0].score));
    history.forEach((p, i) => ctx.lineTo(padding + i * stepX, toY(p.score)));
    ctx.strokeStyle = lastScore >= 0 ? '#22c55e' : '#ef4444';
    ctx.lineWidth = 2;
    ctx.stroke();

    // Current value label
    const last = history[history.length - 1];
    ctx.fillStyle = '#e2e8f0';
    ctx.font = '12px monospace';
    ctx.fillText(`${(last.score / 100).toFixed(2)}`, cw - 80, 16);
  }, [history]);

  return (
    <div className="rounded-lg border border-slate-700 bg-slate-900 p-3">
      <div className="mb-2 text-xs font-medium text-slate-400 uppercase tracking-wider">Evaluation</div>
      <canvas
        ref={canvasRef}
        className="w-full h-32 rounded"
        style={{ width: '100%', height: 128 }}
      />
    </div>
  );
}
