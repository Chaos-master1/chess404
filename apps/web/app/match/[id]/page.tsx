'use client';

import React from 'react';
import { usePlatform } from '../../../src/contexts/PlatformContext';

export default function MatchRoute({ params }: { params: Promise<{ id: string }> }) {
  const platform = usePlatform();
  const resolvedParams = React.use(params);
  const id = resolvedParams.id;
  const prevIdRef = React.useRef<string | null>(null);

React.useEffect(() => {
    if (!id) return;
    // Avoid re-processing the same match ID
    if (prevIdRef.current === id) return;
    prevIdRef.current = id;

    platform.requestedMatchIdRef.current = id;
    platform.setActivePage('Match');
  }, [id, platform.requestedMatchIdRef, platform.setActivePage]);

  return null;
}
