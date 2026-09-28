'use client';
import React from 'react';
import CardsPage from '../../src/CardsPage';
import { usePlatform } from '../../src/contexts/PlatformContext';

export default function CardsRoute() {
  const p = usePlatform();
  return <CardsPage embedded onNavigate={(page: string) => p.setActivePage(page as Parameters<typeof p.setActivePage>[0])} />;
}
