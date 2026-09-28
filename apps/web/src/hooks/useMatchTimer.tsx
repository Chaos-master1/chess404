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
  // timeW/timeB are milliseconds, matching the server's clock.whiteMs/blackMs.
  // initialClockStart (CLOCK_START) is in seconds.
  const [timeW, setTimeW] = React.useState(initialClockStart * 1000);
  const [timeB, setTimeB] = React.useState(initialClockStart * 1000);
  const [clockActive, setClockActive] = React.useState(false);

  const tickingRef = React.useRef<PieceColor | null>(null);
  const [tickingState, setTickingState] = React.useState<PieceColor | null>(null);

  // Use refs for callbacks to avoid stale closures in intervals
  const onTimeoutRef = React.useRef(onTimeout);
  onTimeoutRef.current = onTimeout;

  const setTicking = React.useCallback((v: PieceColor | null) => {
    tickingRef.current = v;
    setTickingState(v);
  }, []);

  const resetTimer = React.useCallback(() => {
    setTimeW(initialClockStart * 1000);
    setTimeB(initialClockStart * 1000);
    setTicking(null);
    setClockActive(false);
  }, [initialClockStart, setTicking]);

  // The abort countdown was removed: aborts are server-decided (aborts are
  // legal before Black's first reply), and the old local 10-second timer was
  // pure theater -- it ticked down to nothing in hosted matches, flashed a
  // fake "must move" banner before the first snapshot, and could even fire a
  // self-abort callback that no longer matches server rules. Clock display is
  // server-driven only; timeW/timeB are updated exclusively from
  // authoritative snapshots or local game ticks.
  void over;
  void authoritativeLive;
  void onTimeoutRef;

  return {
    timeW, setTimeW,
    timeB, setTimeB,
    clockActive, setClockActive,
    tickingState, tickingRef, setTicking,
    resetTimer,
  };
}
