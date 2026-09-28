'use client';

import React from 'react';

/**
 * Fixed viewport background that survives mobile browsers. Chrome on
 * Android renders `background-attachment: fixed` only for the initially
 * painted region, leaving the rest of the page flat — the Cards page
 * showed a hard edge under the first screenful. A position:fixed layer
 * is the portable equivalent (also used by AppShellLayout's <main>).
 */
export function FixedBackground({ overlay, overlayOpacity = 0.55 }: { overlay?: boolean; overlayOpacity?: number }) {
  return (
    <>
      <div
        aria-hidden
        style={{
          position: 'fixed', inset: 0, zIndex: -2, pointerEvents: 'none',
          backgroundImage: 'url(/background.webp)',
          backgroundSize: 'cover',
          backgroundPosition: 'center',
          backgroundRepeat: 'no-repeat',
        }}
      />
      {overlay ? (
        <div
          aria-hidden
          style={{
            position: 'fixed', inset: 0, zIndex: -1, pointerEvents: 'none',
            background: `rgba(6,3,16,${overlayOpacity})`,
          }}
        />
      ) : null}
    </>
  );
}
