'use client';

import React from 'react';
import {
  AccountIcon,
  ReturnIcon,
  ToolsIcon,
} from './icons';

export type ShellNavItem = {
  key: string;
  label: string;
  icon: React.ReactNode;
  badge?: number | null;
};

export type ShellNavGroup = {
  label?: string;
  items: ShellNavItem[];
};

export type ShellPageMeta = {
  eyebrow?: string;
  title: string;
  description?: string;
};

interface AppShellProps {
  brandTitle: string;
  brandSubtitle: string;
  pageMeta: ShellPageMeta;
  primaryItems: ShellNavItem[];
  utilityGroups: ShellNavGroup[];
  accountLabel: string;
  activeKey: string;
  onNavigate: (key: string) => void;
  onOpenAccount: () => void;
  showReturnToMatch?: boolean;
  onReturnToMatch?: () => void;
  topNotice?: React.ReactNode;
  children: React.ReactNode;
  hideBottomNav?: boolean;
}

function SidebarItem({
  item,
  active,
  onClick,
}: {
  item: ShellNavItem;
  active: boolean;
  onClick: () => void;
}): React.ReactElement {
  return (
    <button className={`app-shell__nav-item${active ? ' app-shell__nav-item--active' : ''}`} onClick={onClick} aria-current={active ? 'page' : undefined}>
      <span className="app-shell__nav-main">
        <span className="app-shell__nav-icon">{item.icon}</span>
        <span className="app-shell__nav-text">{item.label}</span>
      </span>
      {item.badge ? <span className="app-shell__nav-badge">{item.badge}</span> : null}
    </button>
  );
}

export default function AppShell({
  brandTitle,
  brandSubtitle,
  pageMeta,
  primaryItems,
  utilityGroups,
  accountLabel,
  activeKey,
  onNavigate,
  onOpenAccount,
  showReturnToMatch = false,
  onReturnToMatch,
  topNotice = null,
  children,
  hideBottomNav = false,
}: AppShellProps): React.ReactElement {
  const [mobileToolsOpen, setMobileToolsOpen] = React.useState(false);

  React.useEffect(() => {
    setMobileToolsOpen(false);
  }, [activeKey]);

  const bottomNavItems = [
    ...(showReturnToMatch && onReturnToMatch ? [{ key: '__return__', label: 'Return', icon: <ReturnIcon /> }] : []),
    ...primaryItems,
    { key: '__account__', label: 'Account', icon: <AccountIcon /> },
  ];

  return (
    <div className="app-root">
      <div className="app-shell">
        <aside className="app-shell__sidebar">
          <div className="app-shell__brand">
            <div className="app-shell__brand-mark">
              {/* logo512.png is the full-art gold mark; logo-mark.png is a
                  mostly-black tile that read as a dark square in the amber
                  brand box. */}
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src="/logo512.png" alt="" className="app-shell__brand-mark-img" />
            </div>
            <div className="app-shell__brand-copy">
              <div className="app-shell__brand-title">{brandTitle}</div>
              <div className="app-shell__brand-subtitle">{brandSubtitle}</div>
            </div>
          </div>

          <div className="app-shell__sidebar-scroll">
            {showReturnToMatch && onReturnToMatch ? (
              <div style={{ padding: '0 0 12px 0' }}>
                <button
                  className="app-shell__nav-item"
                  onClick={onReturnToMatch}
                  style={{
                    width: '100%',
                    background: 'linear-gradient(135deg, rgba(200, 134, 10, 0.4) 0%, rgba(139, 94, 10, 0.5) 100%)',
                    border: '1px solid rgba(255, 180, 60, 0.7)',
                    borderRadius: '10px',
                    color: '#ffd700',
                    fontWeight: 800,
                    boxShadow: '0 4px 16px rgba(200, 134, 10, 0.35)',
                    padding: '10px 14px',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '10px',
                    transition: 'all 0.15s ease',
                  }}
                >
                  <span className="app-shell__nav-icon" style={{ fontSize: '18px' }}><ReturnIcon /></span>
                  <span className="app-shell__nav-text" style={{ fontSize: '13px', letterSpacing: '0.3px' }}>Return to Match</span>
                </button>
              </div>
            ) : null}
            <div className="app-shell__nav-group">
              <div className="app-shell__nav-label">Core</div>
              {primaryItems.map((item) => (
                <SidebarItem
                  key={item.key}
                  item={item}
                  active={activeKey === item.key}
                  onClick={() => onNavigate(item.key)}
                />
              ))}
            </div>

            {utilityGroups.map((group, index) => (
              <div className="app-shell__nav-group" key={`${group.label ?? 'utility'}-${index}`}>
                {group.label ? <div className="app-shell__nav-label">{group.label}</div> : null}
                {group.items.map((item) => (
                  <SidebarItem
                    key={item.key}
                    item={item}
                    active={activeKey === item.key}
                    onClick={() => onNavigate(item.key)}
                  />
                ))}
              </div>
            ))}
          </div>

          <div className="app-shell__sidebar-footer">
            <button className={`app-shell__nav-item${activeKey === 'Account' ? ' app-shell__nav-item--active' : ''}`} onClick={onOpenAccount} aria-current={activeKey === 'Account' ? 'page' : undefined}>
              <span className="app-shell__nav-main">
                <span className="app-shell__nav-icon"><AccountIcon /></span>
                <span className="app-shell__nav-text">{accountLabel}</span>
              </span>
            </button>
          </div>
        </aside>

        <div className="app-shell__content">
          {showReturnToMatch && onReturnToMatch ? (
            <div style={{
              background: 'linear-gradient(90deg, rgba(200, 134, 10, 0.28) 0%, rgba(30, 20, 10, 0.85) 100%)',
              borderBottom: '1px solid rgba(255, 170, 40, 0.4)',
              padding: '10px 24px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: '12px',
              zIndex: 50,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#ffcf72', fontSize: '13px', fontWeight: 700 }}>
                <span style={{ fontSize: '16px' }}>⚔️</span>
                <span>You have an unfinished game in progress</span>
              </div>
              <button
                onClick={onReturnToMatch}
                style={{
                  padding: '7px 16px',
                  borderRadius: '6px',
                  background: 'linear-gradient(180deg, #c8860a 0%, #7a5008 100%)',
                  color: '#fff8e0',
                  fontWeight: 800,
                  fontSize: '12px',
                  border: '1px solid rgba(255, 180, 60, 0.6)',
                  cursor: 'pointer',
                  boxShadow: '0 2px 10px rgba(200, 134, 10, 0.4)',
                }}
              >
                ⚔️ Return to Match
              </button>
            </div>
          ) : null}

          {topNotice}

          <main className="app-shell__main">
            {children}
          </main>
        </div>

        {mobileToolsOpen ? (
          <div className="app-shell__utility-sheet">
            {utilityGroups.map((group, index) => (
              <div className="app-shell__nav-group" key={`${group.label ?? 'utility-mobile'}-${index}`}>
                {group.label ? <div className="app-shell__nav-label">{group.label}</div> : null}
                {group.items.map((item) => (
                  <SidebarItem
                    key={item.key}
                    item={item}
                    active={activeKey === item.key}
                    onClick={() => onNavigate(item.key)}
                  />
                ))}
              </div>
            ))}
          </div>
        ) : null}

        {!hideBottomNav && (
          <nav className="app-shell__bottom-nav">
            {bottomNavItems.map((item) => {
              const isAccount = item.key === '__account__';
              const isReturn = item.key === '__return__';
              const active = isAccount ? activeKey === 'Account' : activeKey === item.key;
              return (
                <button
                  key={item.key}
                  className={active ? 'is-active' : ''}
                  aria-current={active ? 'page' : undefined}
                  onClick={() => {
                    if (isReturn) {
                      onReturnToMatch?.();
                      return;
                    }
                    if (isAccount) {
                      onOpenAccount();
                      return;
                    }
                    onNavigate(item.key);
                  }}
                >
                  <span className="app-shell__nav-icon">{item.icon}</span>
                  <span>{item.label}</span>
                </button>
              );
            })}
          </nav>
        )}
      </div>
    </div>
  );
}
