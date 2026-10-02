'use client';

import React from 'react';
import type { PieceColor } from '@chess404/contracts';
import { CLOCK_START } from '../constants';

export interface UseMatchTimerProps {
  initialClockStart?: number;
  over?: boolean;
  authoritativeLive?: boolean;
  onTimeout?: (loser: PieceColor) => void;
}

export function useMatchTimer({
  initialClockStart = CLOCK_START,
  over = false,
  authoritativeLive = false,
  onTimeout = () => {},
}: UseMatchTimerProps = {}) {
  const initialMs = initialClockStart * 1000;
  const [timeW, setTimeWState] = React.useState(initialMs);
  const [timeB, setTimeBState] = React.useState(initialMs);
  const [clockActive, setClockActiveState] = React.useState(false);

  const tickingRef = React.useRef<PieceColor | null>(null);
  const [tickingState, setTickingState] = React.useState<PieceColor | null>(null);

  // Authoritative base times and timestamp when the base was synchronized
  const baseWRef = React.useRef(initialMs);
  const baseBRef = React.useRef(initialMs);
  const lastSyncRef = React.useRef(typeof performance !== 'undefined' ? performance.now() : Date.now());
  const clockActiveRef = React.useRef(false);

  const onTimeoutRef = React.useRef(onTimeout);
  onTimeoutRef.current = onTimeout;

  // setTimeW: used when receiving authoritative snapshot values or manual sets
  const setTimeW = React.useCallback((valueOrFn: number | ((prev: number) => number)) => {
    setTimeWState(prev => {
      const next = typeof valueOrFn === 'function' ? valueOrFn(prev) : valueOrFn;
      baseWRef.current = next;
      lastSyncRef.current = typeof performance !== 'undefined' ? performance.now() : Date.now();
      return next;
    });
  }, []);

  // setTimeB: used when receiving authoritative snapshot values or manual sets
  const setTimeB = React.useCallback((valueOrFn: number | ((prev: number) => number)) => {
    setTimeBState(prev => {
      const next = typeof valueOrFn === 'function' ? valueOrFn(prev) : valueOrFn;
      baseBRef.current = next;
      lastSyncRef.current = typeof performance !== 'undefined' ? performance.now() : Date.now();
      return next;
    });
  }, []);

  const setClockActive = React.useCallback((active: boolean) => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    // If stopping the clock, freeze the active color at current elapsed time
    if (!active && clockActiveRef.current && tickingRef.current) {
      const elapsed = Math.max(0, now - lastSyncRef.current);
      if (tickingRef.current === 'white') {
        baseWRef.current = Math.max(0, baseWRef.current - elapsed);
        setTimeWState(baseWRef.current);
      } else if (tickingRef.current === 'black') {
        baseBRef.current = Math.max(0, baseBRef.current - elapsed);
        setTimeBState(baseBRef.current);
      }
    }
    lastSyncRef.current = now;
    clockActiveRef.current = active;
    setClockActiveState(active);
  }, []);

  const setTicking = React.useCallback((v: PieceColor | null) => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    // When switching active turns, freeze the outgoing color's base time
    if (clockActiveRef.current && tickingRef.current && tickingRef.current !== v) {
      const elapsed = Math.max(0, now - lastSyncRef.current);
      if (tickingRef.current === 'white') {
        baseWRef.current = Math.max(0, baseWRef.current - elapsed);
        setTimeWState(baseWRef.current);
      } else if (tickingRef.current === 'black') {
        baseBRef.current = Math.max(0, baseBRef.current - elapsed);
        setTimeBState(baseBRef.current);
      }
    }
    lastSyncRef.current = now;
    tickingRef.current = v;
    setTickingState(v);
  }, []);

  const resetTimer = React.useCallback(() => {
    const init = initialClockStart * 1000;
    baseWRef.current = init;
    baseBRef.current = init;
    lastSyncRef.current = typeof performance !== 'undefined' ? performance.now() : Date.now();
    clockActiveRef.current = false;
    setTimeWState(init);
    setTimeBState(init);
    setTickingState(null);
    tickingRef.current = null;
    setClockActiveState(false);
  }, [initialClockStart]);

  // Smooth local ticking interval: interpolates countdown between server snapshots
  React.useEffect(() => {
    if (!clockActive || over || !tickingState) {
      return;
    }

    const interval = window.setInterval(() => {
      const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
      const elapsed = Math.max(0, now - lastSyncRef.current);

      if (tickingRef.current === 'white') {
        const remaining = Math.max(0, baseWRef.current - elapsed);
        setTimeWState(remaining);
        if (remaining === 0 && !authoritativeLive) {
          onTimeoutRef.current?.('white');
        }
      } else if (tickingRef.current === 'black') {
        const remaining = Math.max(0, baseBRef.current - elapsed);
        setTimeBState(remaining);
        if (remaining === 0 && !authoritativeLive) {
          onTimeoutRef.current?.('black');
        }
      }
    }, 100);

    return () => {
      window.clearInterval(interval);
    };
  }, [clockActive, over, tickingState, authoritativeLive]);

  return {
    timeW, setTimeW,
    timeB, setTimeB,
    clockActive, setClockActive,
    tickingState, tickingRef, setTicking,
    resetTimer,
  };
}
