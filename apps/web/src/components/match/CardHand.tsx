'use client';

import React from 'react';
import type { GameCard, PieceColor } from '../../types';
import { MAX_HAND_SIZE, RARITY_STYLE } from '../../constants';
import { useMatchCard } from '../../contexts/MatchCardContext';
import { useMatchState } from '../../contexts/MatchStateContext';

interface CardHandProps {
  hand?: GameCard[];
  playerColor: PieceColor;
  position: 'top' | 'bottom';
}

export default function CardHand({ hand = [], playerColor, position }: CardHandProps) {
  const { selectedCard, setSelectedCard, cardUsedBy, dealPhase, canUseCard } = useMatchCard();
  const { radarActive } = useMatchState();

  const CW = 68, CH = 96;
  const isBottom = position === 'bottom';

  // The fan must fit the space it actually has: a hard 580px container let a
  // full 10-card hand (~506px of cards) spill over the board and side panels
  // on narrow laptop windows. Measure the wrapper and clamp the per-card x
  // step so the widest fan stays inside the container. At the full 580px
  // width this computes the exact same steps as the old fixed formula.
  const fanRef = React.useRef<HTMLDivElement | null>(null);
  const [fanWidth, setFanWidth] = React.useState(580);
  React.useEffect(() => {
    const el = fanRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(entries => {
      const width = entries[0]?.contentRect?.width ?? 0;
      if (width > 0) setFanWidth(width);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const xStep  = hand.length > 1 ? Math.min(54, (Math.max(fanWidth, CW + 40) - CW - 8) / (hand.length - 1), 520 / hand.length) : 0;
  const spread = hand.length > 1 ? Math.min(18, 60 / hand.length)  : 0;

  if (!hand || hand.length === 0) return null;

  return (
    <div ref={fanRef} style={{
      position:'relative', height: isBottom ? '118px' : '98px', width:'min(580px, 100%)',
      display:'flex', alignItems: isBottom ? 'flex-end' : 'flex-start',
      justifyContent:'center',
      marginTop: isBottom ? '4px' : 0, marginBottom: isBottom ? 0 : '4px',
      overflow:'visible', zIndex:0,
    }}>
      {hand.map((card, i) => {
        const key = card.id || `hidden-${i}`;
        const mid   = (hand.length - 1) / 2;
        const angle = hand.length > 1 ? ((i - mid) / Math.max(hand.length - 1, 1)) * spread : 0;
        const yOff  = hand.length > 1 ? Math.min(12, Math.abs(i - mid) * 3) : 0;
        const xOff  = (i - mid) * xStep;
        const isSelected = selectedCard?.id === card.id;
        const isJokerCard = card.mechanic === 'joker';

        if (!isBottom) {
          // Face-down stubs from the server carry no card fields at all; they
          // must never reach the radar renderer (RARITY_STYLE[card.rarity]
          // would throw on an unknown rarity).
          const isHiddenStub = !card.id && !card.mechanic && !card.rarity;
          if (!isHiddenStub) {
            const glow = radarActive
              ? '0 8px 24px rgba(0,0,0,0.85), 0 0 16px rgba(96,165,250,0.5)'
              : '0 8px 20px rgba(0,0,0,0.85), 0 0 12px rgba(168,85,247,0.35)';
            const border = radarActive ? '2px solid #60a5fa' : '1.5px solid rgba(168,85,247,0.6)';
            return (
              <div key={key} style={{
                position:'absolute', top:`${yOff}px`,
                left:`calc(50% + ${xOff}px - ${CW/2}px)`,
                width:`${CW}px`, height:`${CH}px`,
                transform:`rotate(${-angle}deg)`, transformOrigin:'50% -20%',
                borderRadius:'8px',
                boxShadow: glow,
                background:`linear-gradient(160deg, ${card.color} 0%, color-mix(in srgb, ${card.color} 50%, #000) 100%)`,
                border: border, overflow:'hidden', zIndex:i,
                pointerEvents:'none',
                animation: radarActive ? 'radarReveal 0.4s cubic-bezier(0.34,1.56,0.64,1)' : 'none',
              }}>
                {radarActive && <div style={{ position:'absolute', inset:0, background:'rgba(96,165,250,0.08)', zIndex:0 }} />}
                <div style={{ width:'100%', height:'44px', background:`radial-gradient(ellipse at 50% 30%, ${card.accent}44 0%, transparent 70%)`, display:'flex', alignItems:'center', justifyContent:'center', fontSize:'22px', borderBottom:`1px solid ${card.accent}33` }}>{card.icon}</div>
                <div style={{ padding:'2px 3px', fontSize:'7px', fontWeight:700, color:'#fff', textAlign:'center', lineHeight:'1.2' }}>{card.name}</div>
                <div style={{ margin:'2px 4px 0', padding:'1px 3px', background:`${card.accent}33`, border:`1px solid ${card.accent}55`, borderRadius:'3px', fontSize:'6px', color:card.accent, textAlign:'center', fontWeight:700, textTransform:'uppercase' }}>{card.type}</div>
                <div style={{ margin:'1px 4px 0', padding:'1px 2px', border:`1px solid ${RARITY_STYLE[card.rarity].accent}88`, borderRadius:'3px', fontSize:'5.5px', color:RARITY_STYLE[card.rarity].accent, textAlign:'center', fontWeight:800, textTransform:'uppercase' }}>{RARITY_STYLE[card.rarity].label}</div>
                {radarActive && (
                  <div style={{ position:'absolute', top:'2px', left:'2px', fontSize:'8px', background:'rgba(96,165,250,0.9)', borderRadius:'3px', padding:'1px 3px', color:'#fff', fontWeight:800 }}>📡</div>
                )}
              </div>
            );
          }
          return (
            <div key={key} style={{
              position:'absolute', top:`${yOff}px`,
              left:`calc(50% + ${xOff}px - ${CW/2}px)`,
              width:`${CW}px`, height:`${CH}px`,
              transform:`rotate(${-angle}deg)`, transformOrigin:'50% -20%',
              borderRadius:'8px',
              boxShadow:'0 8px 24px rgba(0,0,0,0.85), 0 0 14px rgba(168,85,247,0.3)',
              background:'linear-gradient(150deg, #180f2d 0%, #0d081b 50%, #05030a 100%)',
              border:'1.5px solid rgba(212,175,55,0.65)',
              overflow:'hidden', zIndex:i, pointerEvents:'none',
            }}>
              <div style={{ position:'absolute', inset:'3px', borderRadius:'6px', border:'1px solid rgba(212,175,55,0.35)', pointerEvents:'none' }} />
              <div style={{ position:'absolute', inset:0, backgroundImage:'radial-gradient(circle at 50% 50%, rgba(168,85,247,0.18) 0%, transparent 60%)', pointerEvents:'none' }} />
              <div style={{
                position:'absolute', inset:0, display:'flex', flexDirection:'column',
                alignItems:'center', justifyContent:'center',
              }}>
                <div style={{
                  width: '32px', height: '32px', borderRadius: '50%',
                  border: '1px solid rgba(212,175,55,0.5)',
                  background: 'radial-gradient(circle, rgba(212,175,55,0.15) 0%, rgba(13,8,27,0.8) 100%)',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  boxShadow: '0 0 10px rgba(212,175,55,0.25)',
                }}>
                  <span style={{
                    fontSize: '18px', color: '#ffd700',
                    filter: 'drop-shadow(0 0 6px rgba(255,215,0,0.8))',
                    transform: 'translateY(-1px)',
                  }}>♔</span>
                </div>
                <div style={{
                  fontSize: '5.5px', fontWeight: 900, color: 'rgba(212,175,55,0.8)',
                  letterSpacing: '1.2px', marginTop: '3px', textTransform: 'uppercase',
                }}>
                  404 CHESS
                </div>
              </div>
              <div style={{ position:'absolute', top:'5px', left:'5px', width:'5px', height:'5px', borderTop:'1.5px solid #fbbf24', borderLeft:'1.5px solid #fbbf24' }} />
              <div style={{ position:'absolute', top:'5px', right:'5px', width:'5px', height:'5px', borderTop:'1.5px solid #fbbf24', borderRight:'1.5px solid #fbbf24' }} />
              <div style={{ position:'absolute', bottom:'5px', left:'5px', width:'5px', height:'5px', borderBottom:'1.5px solid #fbbf24', borderLeft:'1.5px solid #fbbf24' }} />
              <div style={{ position:'absolute', bottom:'5px', right:'5px', width:'5px', height:'5px', borderBottom:'1.5px solid #fbbf24', borderRight:'1.5px solid #fbbf24' }} />
            </div>
          );
        }

        const canUse = canUseCard(card, playerColor);
        const alreadyUsedThisTurn = cardUsedBy[playerColor];
        return (
          <div key={isSelected ? `${card.id}-selected` : card.id}
            data-testid={`hand-card-${card.mechanic}`}
            style={{
              position:'absolute', bottom:`${yOff}px`,
              left:`calc(50% + ${xOff}px - ${CW/2}px)`,
              width:`${CW}px`, height:`${CH}px`,
              transform: isSelected ? `rotate(${angle}deg) translateY(-22px) scale(1.08)` : `rotate(${angle}deg)`,
              transformOrigin:'50% 120%',
              cursor: !canUse ? 'not-allowed' : 'pointer',
              transition:'transform 0.18s ease, filter 0.18s ease',
              zIndex: isSelected ? 99 : i + 1,
              filter: isSelected
                ? `brightness(1.3) drop-shadow(0 0 16px ${card.accent}cc)`
                : !canUse ? 'brightness(0.45) saturate(0.3)' : 'none',
              borderRadius:'8px',
              boxShadow: isJokerCard && canUse
                ? `0 6px 20px rgba(0,0,0,0.8), 0 0 20px rgba(245,158,11,0.5), inset 0 1px 0 rgba(255,255,255,0.15)`
                : `0 6px 18px rgba(0,0,0,0.7), inset 0 1px 0 rgba(255,255,255,0.12)`,
              background:`linear-gradient(160deg, ${card.color} 0%, color-mix(in srgb, ${card.color} 55%, #000) 100%)`,
              border: isJokerCard && canUse ? `1.5px solid ${card.accent}cc` : `1.5px solid ${card.accent}66`,
              overflow:'visible',
              animation: isJokerCard && canUse ? 'jokerFloat 3s ease-in-out infinite' : 'none',
            }}
            onPointerDown={e => {
              const el = e.currentTarget as HTMLDivElement;
              el.style.transform = '';
            }}
            onClick={() => {
              if (!canUse) return;
              setSelectedCard(isSelected ? null : card);
            }}
            onMouseEnter={e => {
              if (!canUse || isSelected) return;
              const el = e.currentTarget as HTMLDivElement;
              el.style.transform = `rotate(${angle}deg) translateY(-22px) scale(1.08)`;
              el.style.zIndex = '99';
              const tip = el.querySelector('.card-tooltip') as HTMLElement;
              if (tip) tip.style.display = 'block';
            }}
            onMouseLeave={e => {
              const el = e.currentTarget as HTMLDivElement;
              el.style.transform = isSelected ? `rotate(${angle}deg) translateY(-22px) scale(1.08)` : `rotate(${angle}deg)`;
              el.style.zIndex = String(isSelected ? 99 : i + 1);
              const tip = el.querySelector('.card-tooltip') as HTMLElement;
              if (tip) tip.style.display = 'none';
            }}
          >
            {/* Header: name + element gem */}
            <div style={{
              padding: '3px 4px 1px',
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              background: 'linear-gradient(180deg, rgba(255,255,255,0.08) 0%, transparent 100%)',
              borderBottom: `1px solid ${card.accent}33`,
            }}>
              <div style={{
                fontSize: '7px', fontWeight: 800, color: '#fff',
                whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                maxWidth: `${CW - 18}px`,
                textShadow: '0 1px 3px rgba(0,0,0,0.9)',
              }}>{card.name}</div>
              <div style={{
                width: '6px', height: '6px', borderRadius: '50%',
                background: card.accent,
                boxShadow: `0 0 5px ${card.accent}`,
                flexShrink: 0,
              }} />
            </div>

            {/* Art Box Window */}
            <div style={{
              width: 'calc(100% - 6px)', height: '46px',
              margin: '2px auto 0',
              borderRadius: '5px',
              background: `radial-gradient(circle at 50% 35%, ${card.accent}44 0%, rgba(6,10,18,0.92) 80%)`,
              border: `1px solid ${card.accent}55`,
              boxShadow: `inset 0 1px 6px rgba(0,0,0,0.8), 0 0 8px ${card.accent}22`,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: '22px', position: 'relative', overflow: 'hidden',
            }}>
              <div style={{ filter: `drop-shadow(0 0 6px ${card.accent})` }}>
                {card.icon}
              </div>
              {isJokerCard && canUse && (
                <>
                  {[0,1,2].map(j => (
                    <div key={j} style={{
                      position:'absolute', top:`${5+j*8}px`, left:`${6+j*18}px`,
                      width:'3px', height:'3px', borderRadius:'50%',
                      background:'#f59e0b',
                      animation:`jokerGlitter ${1.2+j*0.4}s ease-in-out infinite`,
                      animationDelay:`${j*0.35}s`, pointerEvents:'none',
                    }}/>
                  ))}
                </>
              )}
            </div>

            {/* Archetype banner */}
            <div style={{
              margin: '3px 4px 0', padding: '1px 3px',
              background: `${card.accent}25`,
              border: `1px solid ${card.accent}66`,
              borderRadius: '3px',
              fontSize: '6px', color: card.accent,
              textAlign: 'center', fontWeight: 800, textTransform: 'uppercase',
              letterSpacing: '0.4px',
            }}>
              {card.type === 'spell' ? 'SPELL' : 'TRAP'}
            </div>

            {/* Rarity footer */}
            <div style={{
              margin: '2px 4px 0', padding: '1px 2px',
              border: `1px solid ${RARITY_STYLE[card.rarity].accent}88`,
              borderRadius: '3px', fontSize: '5.5px',
              color: RARITY_STYLE[card.rarity].accent,
              textAlign: 'center', fontWeight: 800, textTransform: 'uppercase', letterSpacing: '0.3px',
              boxShadow: card.rarity === 'legendary' ? `0 0 6px ${RARITY_STYLE[card.rarity].glow}` : card.rarity === 'epic' ? `0 0 4px ${RARITY_STYLE[card.rarity].glow}` : 'none',
            }}>
              {RARITY_STYLE[card.rarity].label}
            </div>

            {/* Sheen overlay */}
            <div style={{ position:'absolute', inset:0, borderRadius:'8px', background:'linear-gradient(135deg, rgba(255,255,255,0.08) 0%, transparent 50%)', pointerEvents:'none' }} />

            {!canUse && (
              <div style={{ position:'absolute', inset:0, display:'flex', alignItems:'center', justifyContent:'center', borderRadius:'8px', background:'rgba(0,0,0,0.35)' }}>
                <span style={{ fontSize:'15px', opacity:0.8 }}>{alreadyUsedThisTurn ? '✓' : card.type === 'trap' ? '' : '🔒'}</span>
              </div>
            )}
            <div className="card-tooltip" style={{
              display:'none', position:'absolute', bottom:'calc(100% + 8px)', left:'50%',
              transform:'translateX(-50%)', minWidth:'180px', maxWidth:'240px',
              padding:'10px 12px', borderRadius:'10px', zIndex:999,
              background:'linear-gradient(180deg, rgba(10,14,26,0.98) 0%, rgba(6,8,16,0.99) 100%)',
              border:`1px solid ${RARITY_STYLE[card.rarity].accent}88`,
              boxShadow:`0 8px 32px rgba(0,0,0,0.7), 0 0 16px ${RARITY_STYLE[card.rarity].glow}`,
              pointerEvents:'none',
            }}>
              <div style={{ display:'flex', alignItems:'center', gap:'8px', marginBottom:'6px' }}>
                <span style={{ fontSize:'18px' }}>{card.icon}</span>
                <div style={{ fontWeight:800, fontSize:'12px', color:'#fff' }}>{card.name}</div>
              </div>
              <div style={{ display:'flex', gap:'4px', marginBottom:'5px', flexWrap:'wrap' }}>
                <span style={{ padding:'1px 6px', borderRadius:'3px', fontSize:'8px', fontWeight:800, color: RARITY_STYLE[card.rarity].accent, background:`${RARITY_STYLE[card.rarity].accent}22`, border:`1px solid ${RARITY_STYLE[card.rarity].accent}55`, textTransform:'uppercase', letterSpacing:'0.5px' }}>
                  {RARITY_STYLE[card.rarity].label}
                </span>
                <span style={{ padding:'1px 6px', borderRadius:'3px', fontSize:'8px', fontWeight:700, color:card.accent, background:`${card.accent}22`, border:`1px solid ${card.accent}55`, textTransform:'capitalize' }}>
                  {card.mechanic}
                </span>
              </div>
              <div style={{ fontSize:'10px', color:'rgba(200,210,230,0.9)', lineHeight:1.5, fontWeight:500 }}>
                {card.desc}
              </div>
            </div>
          </div>
        );
      })}
      {hand.length === 0 && dealPhase === 'done' && (
        <div style={{ color:'rgba(255,255,255,0.55)', fontSize:'11px', [isBottom ? 'marginBottom' : 'marginTop']:'28px' }}>
          No cards in hand
        </div>
      )}
      {isBottom && hand.length > 0 && (
        <div style={{
          position:'absolute', bottom:'-2px', right:'0',
          background: hand.length >= MAX_HAND_SIZE
            ? 'rgba(231,76,60,0.9)'
            : hand.length >= MAX_HAND_SIZE - 2 ? 'rgba(243,156,18,0.85)' : 'rgba(30,50,80,0.7)',
          color:'#fff', fontSize:'9px', fontWeight:800,
          padding:'2px 7px', borderRadius:'8px', border:'1px solid rgba(255,255,255,0.15)', zIndex:200,
        }}>
          {hand.length}/{MAX_HAND_SIZE}{hand.length >= MAX_HAND_SIZE ? ' 🔴 FULL' : ''}
        </div>
      )}
    </div>
  );
}
