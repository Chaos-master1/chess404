'use client';

import React from 'react';
import type { PieceColor } from '@chess404/contracts';
import { CLOCK_START } from '../constants';
import { clampRunningClock } from '../lib/snapshot-tier';

export interface UseMatchTimerProps {
  initialClockStart?: number;
  over?: boolean | null;
  authoritativeLive?: boolean | null;
  onTimeout?: (loser: PieceColor) => void;
}

export function useMatchTimer({
  initialClockStart = CLOCK_START,
  over = false,
  authoritativeLive = false,
  onTimeout = () => {},
}: UseMatchTimerProps = {}) {
  const isOver = Boolean(over);
  const isAuthoritative = Boolean(authoritativeLive);
  const initialMs = initialClockStart * 1000;
  const [timeW, setTimeWState] = React.useState(initialMs);
  const [timeB, setTimeBState] = React.useState(initialMs);
  const [clockActive, setClockActiveState] = React.useState(false);

  const tickingRef = React.useRef<PieceColor | null>(null);
  const [tickingState, setTickingState] = React.useState<PieceColor | null>(null);

  // Authoritative base times and independent timestamps when each base was synchronized
  const baseWRef = React.useRef(initialMs);
  const baseBRef = React.useRef(initialMs);
  const syncWRef = React.useRef(typeof performance !== 'undefined' ? performance.now() : Date.now());
  const syncBRef = React.useRef(typeof performance !== 'undefined' ? performance.now() : Date.now());
  const clockActiveRef = React.useRef(false);

  const onTimeoutRef = React.useRef(onTimeout);
  onTimeoutRef.current = onTimeout;

  // setTimeW: used when receiving authoritative snapshot values or manual sets
  const setTimeW = React.useCallback((valueOrFn: number | ((prev: number) => number)) => {
    setTimeWState(prev => {
      const next = typeof valueOrFn === 'function' ? valueOrFn(prev) : valueOrFn;
      const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
      if (clockActiveRef.current && tickingRef.current === 'white') {
        const displayed = Math.max(0, baseWRef.current - Math.max(0, now - syncWRef.current));
        const clamped = clampRunningClock(next, displayed);
        baseWRef.current = clamped;
        syncWRef.current = now;
        return clamped;
      }
      baseWRef.current = next;
      syncWRef.current = now;
      return next;
    });
  }, []);

  // setTimeB: used when receiving authoritative snapshot values or manual sets
  const setTimeB = React.useCallback((valueOrFn: number | ((prev: number) => number)) => {
    setTimeBState(prev => {
      const next = typeof valueOrFn === 'function' ? valueOrFn(prev) : valueOrFn;
      const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
      if (clockActiveRef.current && tickingRef.current === 'black') {
        const displayed = Math.max(0, baseBRef.current - Math.max(0, now - syncBRef.current));
        const clamped = clampRunningClock(next, displayed);
        baseBRef.current = clamped;
        syncBRef.current = now;
        return clamped;
      }
      baseBRef.current = next;
      syncBRef.current = now;
      return next;
    });
  }, []);

  // syncClocks: atomic synchronization of both clocks, turn, and active state from authoritative snapshots
  const syncClocks = React.useCallback(({
    whiteMs,
    blackMs,
    turn,
    active,
  }: {
    whiteMs?: number | null;
    blackMs?: number | null;
    turn?: PieceColor | null;
    active?: boolean;
  }) => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    const nextActive = active !== undefined ? active : clockActiveRef.current;
    const nextTurn = turn !== undefined ? turn : tickingRef.current;
    const currentRunning = clockActiveRef.current ? tickingRef.current : null;

    if (typeof whiteMs === 'number') {
      if (currentRunning === 'white' && nextTurn === 'white' && nextActive) {
        const displayed = Math.max(0, baseWRef.current - Math.max(0, now - syncWRef.current));
        const clamped = clampRunningClock(whiteMs, displayed);
        baseWRef.current = clamped;
        syncWRef.current = now;
        setTimeWState(clamped);
      } else {
        baseWRef.current = whiteMs;
        syncWRef.current = now;
        setTimeWState(whiteMs);
      }
    }

    if (typeof blackMs === 'number') {
      if (currentRunning === 'black' && nextTurn === 'black' && nextActive) {
        const displayed = Math.max(0, baseBRef.current - Math.max(0, now - syncBRef.current));
        const clamped = clampRunningClock(blackMs, displayed);
        baseBRef.current = clamped;
        syncBRef.current = now;
        setTimeBState(clamped);
      } else {
        baseBRef.current = blackMs;
        syncBRef.current = now;
        setTimeBState(blackMs);
      }
    }

    clockActiveRef.current = nextActive;
    setClockActiveState(nextActive);
    tickingRef.current = nextTurn;
    setTickingState(nextTurn);
  }, []);

  const setClockActive = React.useCallback((active: boolean) => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    // If stopping the clock, freeze the active color at current elapsed time
    if (!active && clockActiveRef.current && tickingRef.current) {
      if (tickingRef.current === 'white') {
        const elapsed = Math.max(0, now - syncWRef.current);
        baseWRef.current = Math.max(0, baseWRef.current - elapsed);
        syncWRef.current = now;
        setTimeWState(baseWRef.current);
      } else if (tickingRef.current === 'black') {
        const elapsed = Math.max(0, now - syncBRef.current);
        baseBRef.current = Math.max(0, baseBRef.current - elapsed);
        syncBRef.current = now;
        setTimeBState(baseBRef.current);
      }
    } else if (active && !clockActiveRef.current && tickingRef.current) {
      if (tickingRef.current === 'white') {
        syncWRef.current = now;
      } else if (tickingRef.current === 'black') {
        syncBRef.current = now;
      }
    }
    clockActiveRef.current = active;
    setClockActiveState(active);
  }, []);

  const setTicking = React.useCallback((v: PieceColor | null) => {
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    // When switching active turns, freeze the outgoing color's base time
    if (clockActiveRef.current && tickingRef.current && tickingRef.current !== v) {
      if (tickingRef.current === 'white') {
        const elapsed = Math.max(0, now - syncWRef.current);
        baseWRef.current = Math.max(0, baseWRef.current - elapsed);
        syncWRef.current = now;
        setTimeWState(baseWRef.current);
      } else if (tickingRef.current === 'black') {
        const elapsed = Math.max(0, now - syncBRef.current);
        baseBRef.current = Math.max(0, baseBRef.current - elapsed);
        syncBRef.current = now;
        setTimeBState(baseBRef.current);
      }
    }
    if (v === 'white') {
      syncWRef.current = now;
    } else if (v === 'black') {
      syncBRef.current = now;
    }
    tickingRef.current = v;
    setTickingState(v);
  }, []);

  const resetTimer = React.useCallback(() => {
    const init = initialClockStart * 1000;
    const now = typeof performance !== 'undefined' ? performance.now() : Date.now();
    baseWRef.current = init;
    baseBRef.current = init;
    syncWRef.current = now;
    syncBRef.current = now;
    clockActiveRef.current = false;
    setTimeWState(init);
    setTimeBState(init);
    setTickingState(null);
    tickingRef.current = null;
    setClockActiveState(false);
  }, [initialClockStart]);

  // Smooth local ticking interval: interpolates countdown between server snapshots
  React.useEffect(() => {
    if (!clockActive || isOver || !tickingState) {
      return;
    }

    const interval = window.setInterval(() => {
      const now = typeof performance !== 'undefined' ? performance.now() : Date.now();

      if (tickingRef.current === 'white') {
        const elapsed = Math.max(0, now - syncWRef.current);
        const remaining = Math.max(0, baseWRef.current - elapsed);
        setTimeWState(remaining);
        if (remaining === 0 && !isAuthoritative) {
          onTimeoutRef.current?.('white');
        }
      } else if (tickingRef.current === 'black') {
        const elapsed = Math.max(0, now - syncBRef.current);
        const remaining = Math.max(0, baseBRef.current - elapsed);
        setTimeBState(remaining);
        if (remaining === 0 && !isAuthoritative) {
          onTimeoutRef.current?.('black');
        }
      }
    }, 100);

    return () => {
      window.clearInterval(interval);
    };
  }, [clockActive, isOver, tickingState, isAuthoritative]);

  return {
    timeW, setTimeW,
    timeB, setTimeB,
    clockActive, setClockActive,
    tickingState, tickingRef, setTicking,
    syncClocks,
    resetTimer,
  };
}
