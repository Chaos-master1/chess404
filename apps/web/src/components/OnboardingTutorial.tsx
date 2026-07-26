'use client';

import React from 'react';
import type { TutorialState } from '../hooks/useTutorial';

interface Props {
  tutorial: TutorialState;
  activePage: string;
}

const STEPS: Record<string, { icon: string; title: string; body: string; tip?: string }> = {
  welcome: {
    icon: '♟️✨',
    title: 'Welcome to Chess404!',
    body: 'Chess404 fuses classical competitive chess with tactical card powers. Build mana, draw spell cards, and outsmart your opponent with high-level strategic combos.',
    tip: 'Start by playing vs the Computer or Queue Casual to test spell synergies.',
  },
  board: {
    icon: '🏰',
    title: 'The 64-Square Battleground',
    body: 'Piece movement follows international standard chess rules. Drag or click pieces to view legal moves and threats. Spatial hazards like Bombs and Fortress Zones highlight directly on the canvas.',
    tip: 'Touch or click any highlighted square to execute your move or spell target.',
  },
  cards: {
    icon: '🃏',
    title: 'Your Card Hand',
    body: 'Your hand appears below the board (up to 4 cards max). Click a card to preview its targeting requirement, then tap "Play Card" or click a valid board square.',
    tip: 'Hotkeys 1, 2, 3, 4 quickly select cards from your hand during turn actions.',
  },
  mana: {
    icon: '⚡',
    title: 'Mana & Turn Economy',
    body: 'You gain +1 Mana per turn (up to 10 MP max). Spells cost between 2 MP (Shield/Freeze) to 5 MP (Mind Control). Manage your mana reserve for decisive late-game turns.',
    tip: 'You can play 1 card per turn in addition to your standard chess piece move.',
  },
  spells: {
    icon: '🔮',
    title: 'Spell Arsenal',
    body: 'Unleash Piece Shields (2t immunity), Spatial Bombs (3x3 countdown blast), Quantum Teleports, Piece Clones, or 4-turn Fortress Zones to secure victory!',
    tip: 'Checkmate the enemy King or force a time out to win the match.',
  },
};

export function OnboardingTutorial({ tutorial }: Props): React.ReactElement | null {
  const { active, step, next, prev, dismiss } = tutorial;

  if (!active || step === 'complete') return null;

  const s = STEPS[step];
  if (!s) return null;

  const stepKeys = ['welcome', 'board', 'cards', 'mana', 'spells'];
  const currentIndex = stepKeys.indexOf(step);

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 10000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(5, 8, 16, 0.85)',
        backdropFilter: 'blur(6px)',
        animation: 'fadeIn 0.25s ease-out',
        padding: '16px',
      }}
    >
      <style>{`
        @keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
        @keyframes slideUp { from { opacity: 0; transform: translateY(18px); } to { opacity: 1; transform: translateY(0); } }
      `}</style>
      <div
        style={{
          background: 'linear-gradient(165deg, rgba(16, 26, 44, 0.98) 0%, rgba(8, 14, 26, 0.99) 100%)',
          border: '1px solid rgba(255, 190, 90, 0.4)',
          borderRadius: '20px',
          padding: '32px 28px 24px',
          maxWidth: '480px',
          width: '100%',
          boxShadow: '0 24px 64px rgba(0, 0, 0, 0.7), 0 0 40px rgba(255, 190, 90, 0.15)',
          animation: 'slideUp 0.3s cubic-bezier(0.34, 1.56, 0.64, 1)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
        }}
      >
        <div style={{ fontSize: '36px', marginBottom: '8px' }}>{s.icon}</div>
        <h2 style={{ color: '#ffbe5a', fontSize: '20px', fontWeight: 800, textAlign: 'center', margin: '0 0 12px', letterSpacing: '0.5px' }}>
          {s.title}
        </h2>
        <p style={{ color: 'rgba(244, 239, 230, 0.88)', fontSize: '13px', lineHeight: 1.65, textAlign: 'center', margin: '0 0 18px' }}>
          {s.body}
        </p>

        {s.tip && (
          <div
            style={{
              background: 'rgba(255, 190, 90, 0.08)',
              border: '1px solid rgba(255, 190, 90, 0.25)',
              borderRadius: '10px',
              padding: '10px 14px',
              marginBottom: '20px',
              fontSize: '12px',
              color: '#ffe4b5',
              textAlign: 'center',
              fontWeight: 600,
              width: '100%',
              boxSizing: 'border-box',
            }}
          >
            💡 <strong>Pro Tip:</strong> {s.tip}
          </div>
        )}

        <div style={{ display: 'flex', gap: '10px', width: '100%', justifyContent: 'space-between', marginTop: '4px' }}>
          <button
            onClick={dismiss}
            style={{
              padding: '10px 16px',
              background: 'rgba(255, 255, 255, 0.05)',
              color: 'rgba(244, 239, 230, 0.6)',
              border: '1px solid rgba(255, 255, 255, 0.1)',
              borderRadius: '10px',
              cursor: 'pointer',
              fontSize: '12px',
              fontWeight: 600,
            }}
          >
            Skip Tutorial
          </button>

          <div style={{ display: 'flex', gap: '8px' }}>
            {currentIndex > 0 && (
              <button
                onClick={prev}
                style={{
                  padding: '10px 16px',
                  background: 'rgba(255, 255, 255, 0.08)',
                  color: '#f4efe6',
                  border: '1px solid rgba(255, 255, 255, 0.15)',
                  borderRadius: '10px',
                  cursor: 'pointer',
                  fontSize: '13px',
                  fontWeight: 700,
                }}
              >
                ← Back
              </button>
            )}
            <button
              onClick={next}
              style={{
                padding: '10px 24px',
                background: 'linear-gradient(180deg, #c8860a 0%, #7a5008 100%)',
                color: '#fff8e0',
                border: 'none',
                borderRadius: '10px',
                cursor: 'pointer',
                fontSize: '13px',
                fontWeight: 800,
                boxShadow: '0 4px 14px rgba(200, 134, 10, 0.3)',
              }}
            >
              {step === 'spells' ? 'Got It! Let\'s Play ✨' : 'Next →'}
            </button>
          </div>
        </div>

        {/* Step Indicator Dots */}
        <div style={{ display: 'flex', gap: '8px', justifyContent: 'center', marginTop: '20px' }}>
          {stepKeys.map((k) => (
            <div
              key={k}
              style={{
                width: k === step ? '18px' : '8px',
                height: '8px',
                borderRadius: '4px',
                background: k === step ? '#ffbe5a' : 'rgba(255, 255, 255, 0.18)',
                transition: 'all 0.25s ease',
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
