'use client';
import React from 'react';
import { usePlatform } from '../../src/contexts/PlatformContext';

export default function Route() {
  const platform = usePlatform();
  React.useEffect(() => {
    platform.setActivePage('Account');
  }, [platform.setActivePage]); // eslint-disable-line react-hooks/exhaustive-deps -- mount-once page shim; the context object identity is irrelevant here
  return null;
}

