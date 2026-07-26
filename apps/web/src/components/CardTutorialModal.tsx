'use client';

import React from 'react';

interface CardTutorialModalProps {
  isOpen: boolean;
  onClose: () => void;
}

interface CardGuideItem {
  id: string;
  name: string;
  mana: number;
  icon: string;
  type: 'defense' | 'offense' | 'utility' | 'spatial';
  summary: string;
  details: string;
}

const CARDS_GUIDE: CardGuideItem[] = [
  {
    id: 'shield',
    name: 'Piece Shield',
    mana: 2,
    icon: '🛡️',
    type: 'defense',
    summary: 'Grants immunity to capture for 2 turns.',
    details: 'Target any friendly piece. The piece gains a shimmering green energy shield protecting it from all enemy piece captures and spell blasts for 2 full turns.',
  },
  {
    id: 'bomb',
    name: 'Spatial Bomb',
    mana: 3,
    icon: '💣',
    type: 'spatial',
    summary: 'Places a 3x3 countdown explosive grid hazard.',
    details: 'Target an empty square. A ticking spatial bomb appears with a 2-turn detonation timer. When it explodes, all non-king pieces within the 3x3 surrounding zone are destroyed.',
  },
  {
    id: 'freeze',
    name: 'Freeze Square',
    mana: 2,
    icon: '❄️',
    type: 'utility',
    summary: 'Immobilizes target piece for 1 turn.',
    details: 'Target an enemy or friendly piece. The square freezes in ice, preventing the targeted piece from moving or executing abilities on its next turn.',
  },
  {
    id: 'clone',
    name: 'Piece Clone',
    mana: 4,
    icon: '👥',
    type: 'offense',
    summary: 'Duplicates a non-king piece onto an adjacent square.',
    details: 'Select a friendly piece (Pawn, Knight, Bishop, Rook, Queen) and choose a safe adjacent empty square to spawn a duplicate copy.',
  },
  {
    id: 'teleport',
    name: 'Quantum Teleport',
    mana: 3,
    icon: '🌀',
    type: 'utility',
    summary: 'Instantly teleports a friendly piece to any open square.',
    details: 'Select one of your pieces and target any empty non-fortress square on the board to perform an immediate spatial jump.',
  },
  {
    id: 'mindcontrol',
    name: 'Mind Control',
    mana: 5,
    icon: '🔮',
    type: 'offense',
    summary: 'Takes control of an enemy piece for 1 turn.',
    details: 'Temporarily forces an opponent non-king piece to obey your commands for one turn, allowing you to move it into tactical danger.',
  },
  {
    id: 'fortress',
    name: 'Fortress Zone',
    mana: 3,
    icon: '🏰',
    type: 'defense',
    summary: 'Creates a 2x2 protected sanctuary grid.',
    details: 'Creates a 2x2 glowing zone. Friendly pieces inside the zone cannot be captured by enemy pieces for 4 turns.',
  },
];

export function CardTutorialModal({ isOpen, onClose }: CardTutorialModalProps): React.ReactElement | null {
  const [activeTab, setActiveTab] = React.useState<'overview' | 'cards' | 'shortcuts'>('overview');
  const [selectedCardId, setSelectedCardId] = React.useState<string>('shield');

  if (!isOpen) return null;

  const selectedCard = CARDS_GUIDE.find(c => c.id === selectedCardId) ?? CARDS_GUIDE[0];

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 10000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(5, 8, 16, 0.82)',
        backdropFilter: 'blur(8px)',
        padding: '20px',
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'linear-gradient(165deg, rgba(14, 22, 38, 0.98) 0%, rgba(8, 14, 24, 0.99) 100%)',
          border: '1px solid rgba(255, 190, 90, 0.3)',
          borderRadius: '20px',
          width: '100%',
          maxWidth: '720px',
          maxHeight: '90vh',
          display: 'flex',
          flexDirection: 'column',
          boxShadow: '0 24px 64px rgba(0, 0, 0, 0.7), 0 0 40px rgba(255, 190, 90, 0.12)',
          color: '#f4efe6',
          fontFamily: "'Segoe UI', system-ui, sans-serif",
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal Header */}
        <div
          style={{
            padding: '20px 24px 16px',
            borderBottom: '1px solid rgba(255, 190, 90, 0.15)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            background: 'rgba(255, 190, 90, 0.03)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
            <span style={{ fontSize: '24px' }}>♟️</span>
            <div>
              <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 800, color: '#ffbe5a', letterSpacing: '0.5px' }}>
                CHESS404 — Rules & Card Spell Guide
              </h2>
              <span style={{ fontSize: '12px', color: 'rgba(244, 239, 230, 0.6)' }}>
                Master classical tactics amplified by spatial card powers
              </span>
            </div>
          </div>
          <button
            onClick={onClose}
            style={{
              background: 'rgba(255, 255, 255, 0.06)',
              border: '1px solid rgba(255, 255, 255, 0.12)',
              color: '#f4efe6',
              borderRadius: '50%',
              width: '32px',
              height: '32px',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontWeight: 700,
              fontSize: '16px',
            }}
          >
            ✕
          </button>
        </div>

        {/* Tab Selector */}
        <div style={{ display: 'flex', borderBottom: '1px solid rgba(255, 255, 255, 0.08)', background: 'rgba(0,0,0,0.2)' }}>
          {[
            { id: 'overview', label: '🎮 How To Play' },
            { id: 'cards', label: '🃏 Card Spell Deck' },
            { id: 'shortcuts', label: '⌨️ Controls & Safety' },
          ].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id as any)}
              style={{
                flex: 1,
                padding: '12px 16px',
                border: 'none',
                borderBottom: activeTab === tab.id ? '2px solid #ffbe5a' : '2px solid transparent',
                background: activeTab === tab.id ? 'rgba(255, 190, 90, 0.08)' : 'transparent',
                color: activeTab === tab.id ? '#ffbe5a' : 'rgba(244, 239, 230, 0.65)',
                fontWeight: 700,
                fontSize: '13px',
                cursor: 'pointer',
                transition: 'all 0.2s ease',
              }}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Modal Content Body */}
        <div style={{ padding: '24px', overflowY: 'auto', flex: 1 }}>
          {activeTab === 'overview' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '12px' }}>
                <div style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)', borderRadius: '12px', padding: '16px' }}>
                  <div style={{ fontSize: '20px', marginBottom: '6px' }}>⚡ Mana Generation</div>
                  <div style={{ fontSize: '13px', color: 'rgba(244,239,230,0.8)', lineHeight: 1.5 }}>
                    Gain <strong>+1 Mana</strong> at the start of each move (up to 10 max). Mana powers your spell cards!
                  </div>
                </div>
                <div style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)', borderRadius: '12px', padding: '16px' }}>
                  <div style={{ fontSize: '20px', marginBottom: '6px' }}>🃏 Card Hand (Max 4)</div>
                  <div style={{ fontSize: '13px', color: 'rgba(244,239,230,0.8)', lineHeight: 1.5 }}>
                    Draw 1 spell card every 2 turns. Play 1 card per turn before or after making a piece move.
                  </div>
                </div>
              </div>

              <div style={{ background: 'rgba(255, 190, 90, 0.05)', border: '1px solid rgba(255, 190, 90, 0.2)', borderRadius: '12px', padding: '16px' }}>
                <h4 style={{ margin: '0 0 8px', color: '#ffbe5a', fontSize: '14px', fontWeight: 800 }}>
                  👑 Winning Conditions
                </h4>
                <ul style={{ margin: 0, paddingLeft: '20px', fontSize: '13px', color: 'rgba(244, 239, 230, 0.85)', lineHeight: 1.6 }}>
                  <li><strong>Checkmate:</strong> Trap the enemy King with piece moves or spatial hazards.</li>
                  <li><strong>Time Out:</strong> Deplete your opponent's clock to win.</li>
                  <li><strong>Resignation / Abandonment:</strong> Enemy forfeits or disconnects.</li>
                </ul>
              </div>
            </div>
          )}

          {activeTab === 'cards' && (
            <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr', gap: '18px' }}>
              {/* Card List Sidebar */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                {CARDS_GUIDE.map((card) => (
                  <button
                    key={card.id}
                    onClick={() => setSelectedCardId(card.id)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '10px 12px',
                      borderRadius: '10px',
                      border: selectedCardId === card.id ? '1px solid #ffbe5a' : '1px solid rgba(255,255,255,0.06)',
                      background: selectedCardId === card.id ? 'rgba(255, 190, 90, 0.15)' : 'rgba(255,255,255,0.02)',
                      color: selectedCardId === card.id ? '#ffbe5a' : 'rgba(244,239,230,0.8)',
                      cursor: 'pointer',
                      fontSize: '13px',
                      fontWeight: 700,
                      textAlign: 'left',
                    }}
                  >
                    <span>{card.icon} {card.name}</span>
                    <span style={{ fontSize: '11px', padding: '2px 6px', borderRadius: '4px', background: 'rgba(0,0,0,0.4)', color: '#38bdf8' }}>
                      {card.mana} MP
                    </span>
                  </button>
                ))}
              </div>

              {/* Card Detail Display */}
              <div
                style={{
                  background: 'rgba(0, 0, 0, 0.3)',
                  border: '1px solid rgba(255, 190, 90, 0.2)',
                  borderRadius: '14px',
                  padding: '20px',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '12px',
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <span style={{ fontSize: '32px' }}>{selectedCard.icon}</span>
                    <div>
                      <h3 style={{ margin: 0, fontSize: '18px', color: '#ffbe5a', fontWeight: 800 }}>
                        {selectedCard.name}
                      </h3>
                      <span style={{ fontSize: '11px', textTransform: 'uppercase', color: 'rgba(244, 239, 230, 0.5)', fontWeight: 800 }}>
                        {selectedCard.type} SPELL
                      </span>
                    </div>
                  </div>
                  <div
                    style={{
                      background: 'rgba(56, 189, 248, 0.15)',
                      border: '1px solid rgba(56, 189, 248, 0.4)',
                      color: '#38bdf8',
                      padding: '6px 12px',
                      borderRadius: '8px',
                      fontWeight: 800,
                      fontSize: '13px',
                    }}
                  >
                    Cost: {selectedCard.mana} Mana
                  </div>
                </div>

                <div style={{ fontSize: '14px', fontWeight: 600, color: '#f4efe6', lineHeight: 1.4 }}>
                  {selectedCard.summary}
                </div>

                <div style={{ fontSize: '13px', color: 'rgba(244, 239, 230, 0.75)', lineHeight: 1.6, background: 'rgba(255,255,255,0.03)', padding: '12px', borderRadius: '8px' }}>
                  {selectedCard.details}
                </div>
              </div>
            </div>
          )}

          {activeTab === 'shortcuts' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div style={{ fontSize: '13px', color: 'rgba(244, 239, 230, 0.8)' }}>
                Keyboard shortcuts and mobile touch controls enable fast, precise play during intense matches:
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '10px' }}>
                {[
                  { key: '1, 2, 3, 4', action: 'Select Card from Hand' },
                  { key: 'Esc', action: 'Deselect Piece or Cancel Spell' },
                  { key: 'Arrow Keys', action: 'Navigate Board (Screen Readers)' },
                  { key: 'Space / Enter', action: 'Select Square / Confirm Move' },
                  { key: 'Right Click + Drag', action: 'Draw Tactical Analysis Arrow' },
                ].map((item, idx) => (
                  <div
                    key={idx}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '10px 14px',
                      borderRadius: '8px',
                      background: 'rgba(255,255,255,0.03)',
                      border: '1px solid rgba(255,255,255,0.06)',
                    }}
                  >
                    <span style={{ fontSize: '12px', color: 'rgba(244, 239, 230, 0.7)' }}>{item.action}</span>
                    <span style={{ fontFamily: 'monospace', background: 'rgba(255, 190, 90, 0.2)', color: '#ffbe5a', padding: '3px 8px', borderRadius: '4px', fontSize: '12px', fontWeight: 700 }}>
                      {item.key}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Modal Footer */}
        <div
          style={{
            padding: '16px 24px',
            borderTop: '1px solid rgba(255, 190, 90, 0.15)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            background: 'rgba(0, 0, 0, 0.2)',
          }}
        >
          <span style={{ fontSize: '12px', color: 'rgba(244, 239, 230, 0.5)' }}>
            Tip: Press Esc or click outside anytime to close.
          </span>
          <button
            onClick={onClose}
            style={{
              padding: '10px 24px',
              borderRadius: '10px',
              border: 'none',
              background: 'linear-gradient(180deg, #c8860a 0%, #7a5008 100%)',
              color: '#fff8e0',
              fontWeight: 800,
              fontSize: '13px',
              cursor: 'pointer',
              boxShadow: '0 4px 12px rgba(200, 134, 10, 0.3)',
            }}
          >
            Ready to Play! 🚀
          </button>
        </div>
      </div>
    </div>
  );
}
