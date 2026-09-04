'use client';

import React from 'react';
import type { GameCard, PieceColor, Rarity } from '../types';
import { CARD_POOL } from '../cardPool';
import { RARITY_STYLE } from '../constants';
import { PlayerBar } from '../components/match/PlayerBar';
import { AUTHORITATIVE_JOKER_MECHANICS } from './useCardInteraction';

export interface UseMatchUIHelpersProps {
  displayedWhiteName: string;
  displayedBlackName: string;
  displayedWhiteRating: string | number;
  displayedBlackRating: string | number;
  whiteSeatBadge: string | null;
  blackSeatBadge: string | null;
  timeW: number;
  timeB: number;
  tickingState: PieceColor | null;
  clockActive: boolean;
  over: boolean;
  jokerPicker: {
    card: GameCard;
    playerColor: PieceColor;
    filterRarity: Rarity | 'all';
    transforming: boolean;
  } | null;
  setJokerPicker: React.Dispatch<React.SetStateAction<{
    card: GameCard;
    playerColor: PieceColor;
    filterRarity: Rarity | 'all';
    transforming: boolean;
  } | null>>;
  cancelCard: () => void;
  applyJokerTransform: (jokerCard: GameCard, playerColor: PieceColor, chosenTemplate: Omit<GameCard, 'id'>) => void;
  authoritativeMatchIdRef: React.MutableRefObject<string | null>;
  jokerRef: React.RefObject<HTMLDivElement | null>;
}

export function useMatchUIHelpers(props: UseMatchUIHelpersProps) {
  const {
    displayedWhiteName, displayedBlackName, displayedWhiteRating, displayedBlackRating,
    whiteSeatBadge, blackSeatBadge, timeW, timeB, tickingState, clockActive, over,
    jokerPicker, setJokerPicker, cancelCard, applyJokerTransform,
    authoritativeMatchIdRef, jokerRef,
  } = props;

  const fmtClock = React.useCallback((s: number): string => `${Math.floor(s / 60)}:${(s % 60).toString().padStart(2, '0')}`, []);

  const evalStr = React.useCallback((score: number, m: number | null): string => {
    if (m !== null) return m > 0 ? `M${Math.abs(m)}` : `-M${Math.abs(m)}`;
    return `${score > 0 ? '+' : ''}${score.toFixed(2)}`;
  }, []);

  const evalLabel = React.useCallback((score: number, m: number | null): string => {
    if (m !== null) return m > 0 ? 'White forces checkmate' : 'Black forces checkmate';
    if (score >  2) return 'White is winning';
    if (score >  0.5) return 'White is better';
    if (score < -2) return 'Black is winning';
    if (score < -0.5) return 'Black is better';
    return 'Equal position';
  }, []);

  const renderPlayerCard = React.useCallback((seat: PieceColor): React.ReactElement => {
    const isWhiteSeat = seat === 'white';
    const seatName = isWhiteSeat ? displayedWhiteName : displayedBlackName;
    const seatRating = isWhiteSeat ? displayedWhiteRating : displayedBlackRating;
    const seatTime = isWhiteSeat ? timeW : timeB;
    const seatBadge = isWhiteSeat ? whiteSeatBadge : blackSeatBadge;
    const seatTicking = tickingState === seat && clockActive && !over;

    return (
      <PlayerBar
        seat={seat}
        playerName={seatName}
        rating={seatRating}
        timeMs={seatTime * 1000}
        isClockActive={seatTicking}
        seatBadge={seatBadge ?? undefined}
      />
    );
  }, [displayedWhiteName, displayedBlackName, displayedWhiteRating, displayedBlackRating, timeW, timeB, whiteSeatBadge, blackSeatBadge, tickingState, clockActive, over]);

  const renderJokerPicker = React.useCallback(() => {
    if (!jokerPicker) return null;
    const { card: jokerCard, playerColor, filterRarity, transforming } = jokerPicker;
    const rarities: (Rarity | 'all')[] = ['all', 'trash', 'common', 'rare', 'epic', 'legendary'];
    const filteredPool = (filterRarity === 'all'
      ? CARD_POOL
      : CARD_POOL.filter(c => c.rarity === filterRarity))
      .filter(c => !authoritativeMatchIdRef.current || AUTHORITATIVE_JOKER_MECHANICS.has(c.mechanic));

    return (
      <div ref={jokerRef} role="dialog" aria-modal="true" aria-label="Select card" style={{
        position:'fixed', inset:0, zIndex:1000,
        background:'rgba(0,0,0,0.88)',
        backdropFilter:'blur(8px)',
        display:'flex', alignItems:'center', justifyContent:'center',
      }} onClick={e => { if (e.target === e.currentTarget && !transforming) cancelCard(); }}>
        <div style={{
          background:'linear-gradient(160deg, #1a0a2e 0%, #0d0a1e 50%, #0a1020 100%)',
          border:'2px solid rgba(245,158,11,0.6)',
          borderRadius:'20px', padding:'24px',
          width:'680px', maxWidth:'95vw',
          maxHeight:'85vh', overflow:'hidden',
          display:'flex', flexDirection:'column', gap:'16px',
          boxShadow:'0 0 60px rgba(245,158,11,0.3), 0 20px 60px rgba(0,0,0,0.8)',
          animation:'jokerPickerReveal 0.35s cubic-bezier(0.34,1.56,0.64,1)',
        }}>
          {/* Header */}
          <div style={{ display:'flex', alignItems:'center', gap:'14px', borderBottom:'1px solid rgba(245,158,11,0.25)', paddingBottom:'14px' }}>
            <div style={{ fontSize:'40px', animation: transforming ? 'jokerTransform 0.8s ease-in-out' : 'jokerFloat 3s ease-in-out infinite' }}>🃏</div>
            <div style={{ flex:1 }}>
              <div style={{ color:'#f59e0b', fontWeight:800, fontSize:'20px', letterSpacing:'1px' }}>JOKER — Choose Your Transformation</div>
              <div style={{ color:'rgba(200,180,120,0.7)', fontSize:'12px', marginTop:'3px' }}>
                {transforming
                  ? '✨ Transforming...'
                  : `Pick any card from the pool — the Joker will become it instantly.`
                }
              </div>
            </div>
            {!transforming && (
              <button onClick={cancelCard} style={{ width:'32px', height:'32px', borderRadius:'50%', background:'rgba(231,76,60,0.2)', border:'1px solid rgba(231,76,60,0.5)', color:'#e74c3c', fontSize:'16px', cursor:'pointer', display:'flex', alignItems:'center', justifyContent:'center', fontWeight:700 }}>✕</button>
            )}
          </div>

          {/* Rarity filter tabs */}
          <div style={{ display:'flex', gap:'6px', flexWrap:'wrap' }}>
            {rarities.map(r => {
              const style = r === 'all' ? { accent: '#a0b8d8', glow: 'rgba(160,184,216,0.3)', label: 'ALL' } : RARITY_STYLE[r as Rarity];
              const isActive = filterRarity === r;
              const count = r === 'all' ? CARD_POOL.length : CARD_POOL.filter(c => c.rarity === r).length;
              return (
                <button key={r} onClick={() => setJokerPicker(prev => prev ? { ...prev, filterRarity: r } : null)}
                  style={{
                    padding:'4px 12px', borderRadius:'20px', fontSize:'10px', fontWeight:800,
                    cursor:'pointer', border: isActive ? `1px solid ${style.accent}` : '1px solid rgba(255,255,255,0.1)',
                    background: isActive ? `${style.accent}22` : 'rgba(255,255,255,0.03)',
                    color: isActive ? style.accent : 'rgba(200,215,235,0.45)',
                    textTransform:'uppercase', letterSpacing:'0.8px',
                    boxShadow: isActive ? `0 0 10px ${style.glow}` : 'none',
                    transition:'all 0.15s ease',
                  }}>
                  {style.label} ({count})
                </button>
              );
            })}
          </div>

          {/* Card grid */}
          <div style={{ overflowY:'auto', flex:1 }}>
            <div style={{ display:'grid', gridTemplateColumns:'repeat(auto-fill, minmax(90px, 1fr))', gap:'10px', paddingRight:'4px' }}>
              {filteredPool.map((template, idx) => {
                const style = RARITY_STYLE[template.rarity];
                return (
                  <div key={`${template.mechanic}-${idx}`}
                    onClick={() => !transforming && applyJokerTransform(jokerCard, playerColor, template)}
                    style={{
                      background:`linear-gradient(160deg, ${template.color || style.color} 0%, #050810 100%)`,
                      border:`1px solid ${style.accent}44`,
                      borderRadius:'10px', padding:'10px 8px',
                      cursor: transforming ? 'wait' : 'pointer',
                      display:'flex', flexDirection:'column', alignItems:'center', gap:'5px',
                      transition:'all 0.18s cubic-bezier(0.34,1.56,0.64,1)',
                      opacity: transforming ? 0.5 : 1,
                    }}
                    onMouseEnter={e => {
                      if (transforming) return;
                      const el = e.currentTarget as HTMLDivElement;
                      el.style.transform = 'scale(1.1) translateY(-4px)';
                      el.style.border = `1px solid ${style.accent}cc`;
                      el.style.boxShadow = `0 8px 24px rgba(0,0,0,0.5), 0 0 16px ${style.glow}`;
                    }}
                    onMouseLeave={e => {
                      const el = e.currentTarget as HTMLDivElement;
                      el.style.transform = 'scale(1) translateY(0)';
                      el.style.border = `1px solid ${style.accent}44`;
                      el.style.boxShadow = 'none';
                    }}
                  >
                    <div style={{ fontSize:'24px', lineHeight:1 }}>{template.icon}</div>
                    <div style={{ color:'#fff', fontWeight:700, fontSize:'7.5px', textAlign:'center', lineHeight:'1.3' }}>{template.name}</div>
                    <div style={{
                      padding:'1px 6px', borderRadius:'3px', fontSize:'6px', fontWeight:800,
                      color: style.accent, background:`${style.accent}22`,
                      border:`1px solid ${style.accent}44`,
                      textTransform:'uppercase', letterSpacing:'0.5px',
                    }}>{style.label}</div>
                    <div style={{ fontSize:'6px', color:'rgba(160,184,216,0.5)', textAlign:'center', lineHeight:'1.3', maxHeight:'24px', overflow:'hidden' }}>{template.desc.slice(0, 50)}{template.desc.length > 50 ? '…' : ''}</div>
                  </div>
                );
              })}
            </div>
          </div>

          {transforming && (
            <div style={{ textAlign:'center', padding:'12px', background:'rgba(245,158,11,0.1)', border:'1px solid rgba(245,158,11,0.4)', borderRadius:'10px' }}>
              <div style={{ fontSize:'28px', animation:'jokerSpin 0.8s linear' }}>🃏</div>
              <div style={{ color:'#f59e0b', fontWeight:700, fontSize:'13px', marginTop:'6px' }}>✨ Transforming the Joker...</div>
            </div>
          )}
        </div>
      </div>
    );
  }, [jokerPicker, authoritativeMatchIdRef, jokerRef, cancelCard, setJokerPicker, applyJokerTransform]);

  return {
    fmtClock,
    evalStr,
    evalLabel,
    renderPlayerCard,
    renderJokerPicker,
  };
}
