'use client';

import React from 'react';

// ── Sound engine ──────────────────────────────────────────────────────────────
// Plays the bundled Lichess standard sound set (apps/web/public/sounds/
// lichess-standard/, AGPL-3.0-or-later, see the README next to the files)
// through a small pool of HTMLAudioElement clones per sound so overlapping
// plays (fast moves, simultaneous notifications) never cut each other off.
//
// Browsers block audible playback until a user gesture; we optimistically
// create/pool elements and "unlock" them on the first gesture by attempting an
// inaudible play. Every call site is guarded so audio failures can never break
// the game UI.

type SoundType = 'move' | 'capture' | 'check' | 'timer_warning' | 'game_over' | 'card_play' | 'chat' | 'error';

export interface GameOverSoundOptions {
  /** Who won, from the viewer's perspective. Omit for neutral "game ended". */
  result?: 'win' | 'loss' | 'draw';
}

const SOUND_DIR = '/sounds/lichess-standard';

const SOUND_SRC: Record<SoundType, string> = {
  move: `${SOUND_DIR}/Move.mp3`,
  capture: `${SOUND_DIR}/Capture.mp3`,
  check: `${SOUND_DIR}/Check.mp3`,
  timer_warning: `${SOUND_DIR}/LowTime.mp3`,
  game_over: `${SOUND_DIR}/Victory.mp3`, // overridden per-result at play time
  card_play: `${SOUND_DIR}/Confirmation.mp3`,
  chat: `${SOUND_DIR}/GenericNotify.mp3`,
  error: `${SOUND_DIR}/Error.mp3`,
};

const GAME_OVER_RESULT_SRC: Record<NonNullable<GameOverSoundOptions['result']>, string> = {
  win: `${SOUND_DIR}/Victory.mp3`,
  loss: `${SOUND_DIR}/Defeat.mp3`,
  draw: `${SOUND_DIR}/Draw.mp3`,
};

/** Per-sound volume (relative to the global user volume, 0..1). */
const SOUND_VOLUME: Record<SoundType, number> = {
  move: 0.9,
  capture: 0.9,
  check: 0.9,
  timer_warning: 0.8,
  game_over: 1.0,
  card_play: 0.9,
  chat: 0.7,
  error: 0.8,
};

/** Number of pooled <audio> clones per sound; round-robin across them. */
const POOL_SIZE = 3;
/** Debounce so StrictMode double-fired effects don't sound twice. */
const SAME_SOUND_COOLDOWN_MS = 60;

const audioPool = new Map<SoundType, HTMLAudioElement[]>();
const poolIndex = new Map<SoundType, number>();
const lastPlayedAt = new Map<SoundType, number>();

let soundEnabled = true;
let soundVolume = 1;
let unlocked = false;

export function setSoundEnabled(enabled: boolean) {
  soundEnabled = enabled;
}

export function isSoundEnabled(): boolean {
  return soundEnabled;
}

export function setSoundVolume(volume: number) {
  soundVolume = Math.min(1, Math.max(0, volume));
}

export function isSoundMuted(): boolean {
  return !soundEnabled || soundVolume <= 0;
}

function poolFor(type: SoundType): HTMLAudioElement[] {
  let pool = audioPool.get(type);
  if (!pool) {
    pool = [];
    for (let i = 0; i < POOL_SIZE; i++) {
      const audio = new Audio(SOUND_SRC[type]);
      audio.preload = 'auto';
      pool.push(audio);
    }
    audioPool.set(type, pool);
  }
  return pool;
}

function nextFromPool(type: SoundType): HTMLAudioElement {
  const pool = poolFor(type);
  const index = (poolIndex.get(type) ?? 0) % pool.length;
  poolIndex.set(type, index + 1);
  return pool[index];
}

function attemptUnlock() {
  if (unlocked) return;
  unlocked = true;
  // Browsers require a gesture to start audio; playing one pooled element
  // inaudibly here unlocks the rest. Failures are fine: every play attempt
  // retries via play().catch anyway, and the next real gesture usually wins.
  try {
    const audio = nextFromPool('move');
    audio.muted = true;
    audio.volume = 0;
    const p = audio.play();
    if (p) {
      p.then(() => {
        audio.muted = false;
        audio.pause();
        audio.currentTime = 0;
      }).catch(() => {
        audio.muted = false;
      });
    }
  } catch {}
}

if (typeof document !== 'undefined') {
  const gestureEvents = ['click', 'touchstart', 'keydown', 'pointerdown'];
  for (const ev of gestureEvents) {
    document.addEventListener(ev, attemptUnlock, { once: false, passive: true });
  }
}

function resolveSrc(type: SoundType, options?: GameOverSoundOptions): string {
  if (type === 'game_over' && options?.result && GAME_OVER_RESULT_SRC[options.result]) {
    return GAME_OVER_RESULT_SRC[options.result];
  }
  return SOUND_SRC[type];
}

export function playSound(type: SoundType, options?: GameOverSoundOptions) {
  if (!soundEnabled || soundVolume <= 0 || typeof window === 'undefined') return;
  if (typeof document !== 'undefined') attemptUnlock();
  const now = Date.now();
  if (now - (lastPlayedAt.get(type) ?? 0) < SAME_SOUND_COOLDOWN_MS) return;
  lastPlayedAt.set(type, now);
  try {
    const audio = nextFromPool(type);
    const src = resolveSrc(type, options);
    if (audio.currentSrc !== src && !audio.src.endsWith(src)) {
      audio.src = src;
    }
    audio.volume = Math.min(1, SOUND_VOLUME[type] * soundVolume);
    audio.currentTime = 0;
    const p = audio.play();
    if (p) p.catch(() => {}); // autoplay refusal or decode hiccup: stay silent, never throw
  } catch {}
}

export function useSound() {
  const [enabled, setEnabled] = React.useState(true);
  const [volume, setVolume] = React.useState(1);

  const toggle = React.useCallback(() => {
    setEnabled(prev => {
      const next = !prev;
      setSoundEnabled(next);
      if (next) playSound('move');
      return next;
    });
  }, []);

  const changeVolume = React.useCallback((next: number) => {
    setVolume(next);
    setSoundVolume(next);
  }, []);

  React.useEffect(() => {
    setSoundEnabled(enabled);
  }, [enabled]);

  React.useEffect(() => {
    setSoundVolume(volume);
  }, [volume]);

  return { soundEnabled: enabled, setSoundEnabled: setEnabled, toggleSound: toggle, soundVolume: volume, setSoundVolume: changeVolume };
}
