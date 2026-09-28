'use client';

import React from 'react';
import { useRouter } from 'next/navigation';
import { DEFAULT_MATCH_MODE_ID, OFFICIAL_MATCH_MODES, type MatchModeId, type PieceColor } from '@chess404/contracts';
import { acceptDirectChallenge, sendDirectChallenge, type DirectChallengeLaunchResponse } from './lib/direct-challenge-service';
import { modeLabel } from './lib/match-labels';
import {
  type AccountProfile,
  blockAccount,
  cancelDirectChallenge,
  declineDirectChallenge,
  fetchDirectChallengeOverview,
  fetchFriendOverview,
  removeFriend,
  respondToFriendRequest,
  sendFriendRequest,
  type DirectChallengeOverview,
  type DirectChallengeView,
  type FriendOverview,
  type FriendRequestView,
  type FriendshipView,
} from './lib/platform-service';
import { writeStoredRoomMeta } from './lib/match-service';
import type { PrivateMatchIdentity } from './lib/private-match-service';
import { formatDateTime } from './lib/display';
import InboxPage from './InboxPage';

// Time controls offered in the challenge popup. Kept in sync with
// QueuePage's QUEUE_CLOCK_OPTIONS deliberately NOT imported: a named import
// would pull the whole queue page module into the Friends bundle.
interface ChallengeClockOption {
  seconds: number;
  increment: number;
  label: string;
}

// Index into CHALLENGE_CLOCK_OPTIONS: 10+0 Rapid, the house default.
const DEFAULT_CHALLENGE_CLOCK_INDEX = 1;

const CHALLENGE_CLOCK_OPTIONS: ChallengeClockOption[] = [
  { seconds: 300, increment: 0, label: '5+0 Blitz' },
  { seconds: 600, increment: 0, label: '10+0 Rapid' },
  { seconds: 900, increment: 10, label: '15+10 Classic' },
  { seconds: 1800, increment: 0, label: '30+0 Long' },
];

interface FriendsPageProps {
  identity?: PrivateMatchIdentity | null;
  accountId?: string | null;
  sessionToken?: string | null;
  liveRefreshToken?: number;
  onOpenProfile?: (handle: string) => void;
  onOpenAccount?: () => void;
  onUnreadCountChange?: (count: number) => void;
}


function describePresence(account: AccountProfile): {
  label: string;
  detail: string;
  border: string;
  background: string;
  color: string;
} {
  switch (account.presenceStatus) {
    case 'online':
      return {
        label: 'Online now',
        detail: 'Ready for live play',
        border: '1px solid rgba(86,204,120,0.28)',
        background: 'rgba(30,110,60,0.18)',
        color: '#d8ffe5',
      };
    case 'recently_active':
      return {
        label: 'Recently active',
        detail: account.lastActiveAt ? `Active ${formatDateTime(account.lastActiveAt)}` : 'Seen recently',
        border: '1px solid rgba(255,180,60,0.24)',
        background: 'rgba(255,180,60,0.08)',
        color: '#ffe7a9',
      };
    default:
      return {
        label: 'Offline',
        detail: `Last seen ${formatDateTime(account.lastSeenAt)}`,
        border: '1px solid rgba(255,255,255,0.12)',
        background: 'rgba(255,255,255,0.035)',
        color: 'rgba(255,232,180,0.72)',
      };
  }
}

function persistChallengeRoom(result: DirectChallengeLaunchResponse): void {
  const matchSnapshot = result.match.snapshot?.match;
  writeStoredRoomMeta(result.match.matchId, {
    queue: 'direct',
    modeId: result.modeId ?? matchSnapshot?.modeId ?? DEFAULT_MATCH_MODE_ID,
    viewerSeat: result.match.seatColor,
    whiteGuestId: matchSnapshot?.whiteGuestId,
    blackGuestId: matchSnapshot?.blackGuestId,
    whiteAccountId: matchSnapshot?.whiteAccountId,
    blackAccountId: matchSnapshot?.blackAccountId,
    whiteName: matchSnapshot?.whiteName,
    blackName: matchSnapshot?.blackName,
    whitePlayerSecret: result.match.seatColor === 'white' ? result.match.claim?.playerSecret : undefined,
    blackPlayerSecret: result.match.seatColor === 'black' ? result.match.claim?.playerSecret : undefined,
    whiteClaimToken: result.match.seatColor === 'white' ? result.match.claim?.claimToken : undefined,
    blackClaimToken: result.match.seatColor === 'black' ? result.match.claim?.claimToken : undefined,
    whiteClaimExpiresAt: result.match.seatColor === 'white' ? result.match.claim?.expiresAt : undefined,
    blackClaimExpiresAt: result.match.seatColor === 'black' ? result.match.claim?.expiresAt : undefined,
  });
}

export default function FriendsPage({
  identity = null,
  accountId = null,
  sessionToken = null,
  liveRefreshToken = 0,
  onOpenProfile,
  onOpenAccount,
  onUnreadCountChange,
}: FriendsPageProps): React.ReactElement {
  const router = useRouter();
  const [overview, setOverview] = React.useState<FriendOverview | null>(null);
  const [challengeOverview, setChallengeOverview] = React.useState<DirectChallengeOverview | null>(null);
  const [targetHandle, setTargetHandle] = React.useState('');
  const [challengeModeId, setChallengeModeId] = React.useState<MatchModeId>(DEFAULT_MATCH_MODE_ID);
  // Lichess-style challenge flow: clicking Challenge opens a popup that
  // carries mode, time control, and color (with a random option the old
  // fixed-defaults section never had). challengeColor resolves to a real
  // seat at send time.
  const [challengeTarget, setChallengeTarget] = React.useState<FriendshipView | null>(null);
  const [challengeColor, setChallengeColor] = React.useState<'black' | 'random' | 'white'>('white');
  const [challengeClock, setChallengeClock] = React.useState(CHALLENGE_CLOCK_OPTIONS[DEFAULT_CHALLENGE_CLOCK_INDEX]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState('');
  const [notice, setNotice] = React.useState('');
  const [busyRequestId, setBusyRequestId] = React.useState<string | null>(null);

  const loadOverview = React.useCallback(async () => {
    if (!accountId || !sessionToken) {
      setOverview(null);
      setChallengeOverview(null);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const [nextFriends, nextChallenges] = await Promise.all([
        fetchFriendOverview({ accountId, sessionToken }),
        fetchDirectChallengeOverview({ accountId, sessionToken }),
      ]);
      setOverview(nextFriends);
      setChallengeOverview(nextChallenges);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load friends.');
    } finally {
      setLoading(false);
    }
  }, [accountId, sessionToken]);

  React.useEffect(() => {
    if (busyRequestId) {
      return;
    }
    void loadOverview();
  }, [busyRequestId, liveRefreshToken, loadOverview]);

  const mutateOverview = React.useCallback((next: FriendOverview, message?: string) => {
    setOverview(next);
    setError('');
    if (message) {
      setNotice(message);
    }
  }, []);

  const mutateChallenges = React.useCallback((next: DirectChallengeOverview, message?: string) => {
    setChallengeOverview(next);
    setError('');
    if (message) {
      setNotice(message);
    }
  }, []);

  const submitFriendRequest = React.useCallback(async () => {
    if (!accountId || !sessionToken) {
      setError('Sign in to send friend requests.');
      return;
    }
    const handle = targetHandle.trim().toLowerCase();
    if (!handle) {
      setError('Enter a handle to send a friend request.');
      return;
    }
    setBusyRequestId('send');
    setNotice('');
    setError('');
    try {
      const next = await sendFriendRequest({ accountId, sessionToken, targetHandle: handle });
      mutateOverview(next, `Friend request sent to @${handle}`);
      setTargetHandle('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send friend request.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, mutateOverview, sessionToken, targetHandle]);

  const handleRespond = React.useCallback(async (request: FriendRequestView, accept: boolean) => {
    if (!accountId || !sessionToken) {
      setError('Sign in to manage friend requests.');
      return;
    }
    setBusyRequestId(request.requestId);
    setNotice('');
    setError('');
    try {
      const next = await respondToFriendRequest({
        accountId,
        sessionToken,
        requestId: request.requestId,
        accept,
      });
      mutateOverview(next, accept ? `You are now friends with @${request.account.handle}` : `Declined @${request.account.handle}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update friend request.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, mutateOverview, sessionToken]);

  const handleRemoveFriend = React.useCallback(async (friendship: FriendshipView) => {
    if (!accountId || !sessionToken) {
      setError('Sign in to manage friends.');
      return;
    }
    setBusyRequestId(friendship.friendshipId);
    setNotice('');
    setError('');
    try {
      const next = await removeFriend({
        accountId,
        sessionToken,
        friendAccountId: friendship.account.accountId,
      });
      mutateOverview(next, `Removed @${friendship.account.handle} from your friends list`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove friend.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, mutateOverview, sessionToken]);

  const handleBlockFriend = React.useCallback(async (friendship: FriendshipView) => {
    if (!accountId || !sessionToken) {
      setError('Sign in to manage friends.');
      return;
    }
    if (!window.confirm(`Block @${friendship.account.handle}? They will be removed from your friends list.`)) {
      return;
    }
    setBusyRequestId(`block:${friendship.friendshipId}`);
    setNotice('');
    setError('');
    try {
      await blockAccount({
        accountId,
        sessionToken,
        targetAccountId: friendship.account.accountId,
      });
      setOverview(prev => prev ? { ...prev, friends: prev.friends.filter(f => f.friendshipId !== friendship.friendshipId) } : null);
      setNotice(`Blocked @${friendship.account.handle}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to block friend.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, sessionToken]);

  const handleSendChallenge = React.useCallback(async (
    friendship: FriendshipView,
    choices: { modeId: MatchModeId; seat: PieceColor; clockSeconds: number },
  ) => {
    if (!accountId || !sessionToken || !identity?.guestId) {
      setError('Sign in with an active player session to send direct challenges.');
      return;
    }
    setBusyRequestId(`challenge:${friendship.friendshipId}`);
    setNotice('');
    setError('');
    try {
      const result = await sendDirectChallenge({
        identity,
        targetAccountId: friendship.account.accountId,
        modeId: choices.modeId,
        preferredSeat: choices.seat,
        clockSeconds: choices.clockSeconds,
      });
      persistChallengeRoom(result);
      setChallengeTarget(null);
      router.push(`/match/${encodeURIComponent(result.match.matchId)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send direct challenge.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, identity, sessionToken, router]);

  const handleAcceptChallenge = React.useCallback(async (challenge: DirectChallengeView) => {
    if (!accountId || !sessionToken || !identity?.guestId) {
      setError('Sign in with an active player session to accept direct challenges.');
      return;
    }
    setBusyRequestId(`accept:${challenge.challengeId}`);
    setNotice('');
    setError('');
    try {
      const result = await acceptDirectChallenge({
        challengeId: challenge.challengeId,
        identity,
      });
      persistChallengeRoom(result);
      router.push(`/match/${encodeURIComponent(result.match.matchId)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to accept direct challenge.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, identity, sessionToken, router]);

  const handleDeclineChallenge = React.useCallback(async (challenge: DirectChallengeView) => {
    if (!accountId || !sessionToken) {
      setError('Sign in to manage direct challenges.');
      return;
    }
    setBusyRequestId(`decline:${challenge.challengeId}`);
    setNotice('');
    setError('');
    try {
      await declineDirectChallenge({
        accountId,
        sessionToken,
        challengeId: challenge.challengeId,
      });
      const next = await fetchDirectChallengeOverview({ accountId, sessionToken });
      mutateChallenges(next, `Declined @${challenge.account.handle}'s challenge.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to decline direct challenge.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, mutateChallenges, sessionToken]);

  const handleCancelChallenge = React.useCallback(async (challenge: DirectChallengeView) => {
    if (!accountId || !sessionToken) {
      setError('Sign in to manage direct challenges.');
      return;
    }
    setBusyRequestId(`cancel:${challenge.challengeId}`);
    setNotice('');
    setError('');
    try {
      await cancelDirectChallenge({
        accountId,
        sessionToken,
        challengeId: challenge.challengeId,
      });
      const next = await fetchDirectChallengeOverview({ accountId, sessionToken });
      mutateChallenges(next, `Cancelled your challenge to @${challenge.account.handle}.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to cancel direct challenge.');
    } finally {
      setBusyRequestId(null);
    }
  }, [accountId, mutateChallenges, sessionToken]);

  const renderProfileChip = React.useCallback((handle: string, label?: string) => (
    <button
      onClick={() => onOpenProfile?.(handle)}
      style={{
        padding: '7px 10px',
        borderRadius: '999px',
        border: '1px solid rgba(255,180,60,0.22)',
        background: 'rgba(255,180,60,0.08)',
        color: '#ffe7a9',
        fontSize: '11px',
        fontWeight: 800,
        cursor: 'pointer',
      }}
    >
      {label ?? `@${handle}`}
    </button>
  ), [onOpenProfile]);

  if (!accountId || !sessionToken) {
    return (
      <div style={{ display: 'flex', flex: 1, minHeight: 0, alignItems: 'center', justifyContent: 'center', padding: '32px' }}>
        <div style={{
          width: 'min(560px, 100%)',
          padding: '26px',
          borderRadius: '18px',
          border: '1px solid rgba(255,180,60,0.18)',
          background: 'linear-gradient(180deg, rgba(15,18,28,0.98) 0%, rgba(10,12,20,0.96) 100%)',
          boxShadow: '0 20px 60px rgba(0,0,0,0.35)',
          color: '#fff2c8',
        }}>
          <div style={{ fontSize: '13px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase', color: '#ffcf72' }}>Friends</div>
          <div style={{ marginTop: '10px', fontSize: '15px', lineHeight: 1.7, color: 'rgba(255,236,194,0.82)' }}>
            Friends are tied to real account sessions now. Sign into a claimed account to send requests, accept friends, and unlock future direct challenges and lobby invites.
          </div>
          <div style={{ display: 'flex', gap: '10px', marginTop: '18px', flexWrap: 'wrap' }}>
            <button
              onClick={onOpenAccount}
              style={{
                padding: '11px 16px',
                borderRadius: '10px',
                border: '1px solid rgba(255,180,60,0.36)',
                background: 'linear-gradient(180deg, rgba(200,134,10,0.36) 0%, rgba(122,79,8,0.44) 100%)',
                color: '#fff4d3',
                fontSize: '12px',
                fontWeight: 800,
                cursor: 'pointer',
              }}
            >
              Open Account
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flex: 1, minHeight: 0, padding: '22px 28px 26px', gap: '18px' }}>
      <div style={{
        width: '390px',
        flexShrink: 0,
        minWidth: 0,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'linear-gradient(180deg, rgba(14,18,30,0.98) 0%, rgba(9,12,20,0.96) 100%)',
        border: '1px solid rgba(255,165,40,0.16)',
        borderRadius: '14px',
        boxShadow: '0 12px 40px rgba(0,0,0,0.35)',
        overflow: 'hidden',
      }}>
        <div style={{ padding: '18px 20px 14px', borderBottom: '1px solid rgba(255,165,40,0.12)' }}>
          <div style={{ color: '#ffcf72', fontSize: '13px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase' }}>Friends Network</div>
          <div style={{ color: 'rgba(255,232,180,0.72)', fontSize: '12px', marginTop: '4px', lineHeight: 1.5 }}>
            Send friend requests by handle, manage direct challenges, and keep a persistent social graph tied to your Chess404 account.
          </div>
          {overview && (
            <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginTop: '14px', flexWrap: 'wrap' }}>
              {renderProfileChip(overview.viewer.handle, `Signed in as @${overview.viewer.handle}`)}
              <div style={{ fontSize: '11px', color: 'rgba(255,232,180,0.64)' }}>
                {overview.friends.length} friends
              </div>
            </div>
          )}

          <div style={{ display: 'grid', gap: '10px', marginTop: '14px' }}>
            <div style={{ display: 'flex', gap: '8px' }}>
              <input
                aria-label="Send friend request"
                value={targetHandle}
                onChange={(event) => setTargetHandle(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    void submitFriendRequest();
                  }
                }}
                placeholder="Send request to handle"
                style={{
                  flex: 1,
                  padding: '10px 12px',
                  borderRadius: '10px',
                  border: '1px solid rgba(255,180,60,0.24)',
                  background: 'rgba(255,255,255,0.04)',
                  color: '#fff2c8',
                  fontSize: '12px',
                  fontWeight: 700,
                  outline: 'none',
                }}
              />
              <button
                onClick={() => void submitFriendRequest()}
                disabled={busyRequestId === 'send'}
                style={{
                  minHeight: '42px',
                  padding: '10px 16px',
                  borderRadius: '10px',
                  border: '1px solid rgba(255,180,60,0.3)',
                  background: 'linear-gradient(180deg, rgba(200,134,10,0.28) 0%, rgba(122,79,8,0.38) 100%)',
                  color: '#fff2c8',
                  fontSize: '12px',
                  fontWeight: 800,
                  cursor: 'pointer',
                  opacity: busyRequestId === 'send' ? 0.7 : 1,
                }}
              >
                Send
              </button>
            </div>
            <button
              onClick={() => void loadOverview()}
              style={{
                justifySelf: 'start',
                minHeight: '40px',
                padding: '9px 16px',
                borderRadius: '8px',
                border: '1px solid rgba(255,180,60,0.2)',
                background: 'rgba(255,255,255,0.04)',
                color: '#fff2c8',
                fontSize: '11px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              Refresh overview
            </button>

          </div>
        </div>

        <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '18px 20px 22px', display: 'grid', gap: '14px' }}>
          {error && (
            <div style={{ padding: '12px 14px', borderRadius: '10px', background: 'rgba(120,20,20,0.22)', border: '1px solid rgba(231,76,60,0.32)', color: '#ffb1a7', fontSize: '12px', fontWeight: 700 }}>
              {error}
            </div>
          )}
          {notice && (
            <div style={{ padding: '12px 14px', borderRadius: '10px', background: 'rgba(20,90,50,0.22)', border: '1px solid rgba(80,190,120,0.28)', color: '#d7ffd7', fontSize: '12px', fontWeight: 700 }}>
              {notice}
            </div>
          )}

          <FriendSection title="Incoming Requests" emptyLabel="No incoming requests right now.">
            {overview?.incoming.map((request) => (
              <FriendRequestCard
                key={request.requestId}
                request={request}
                busy={busyRequestId === request.requestId}
                onOpenProfile={onOpenProfile}
                onAccept={() => void handleRespond(request, true)}
                onDecline={() => void handleRespond(request, false)}
              />
            ))}
          </FriendSection>

          <FriendSection title="Outgoing Requests" emptyLabel="No pending outgoing requests.">
            {overview?.outgoing.map((request) => (
              <FriendRequestCard
                key={request.requestId}
                request={request}
                busy={busyRequestId === request.requestId}
                onOpenProfile={onOpenProfile}
                readOnly
              />
            ))}
          </FriendSection>

          <FriendSection title="Incoming Challenges" emptyLabel="No incoming direct challenges.">
            {challengeOverview?.incoming.map((challenge) => (
              <DirectChallengeCard
                key={challenge.challengeId}
                challenge={challenge}
                busy={busyRequestId === `accept:${challenge.challengeId}` || busyRequestId === `decline:${challenge.challengeId}`}
                onOpenProfile={onOpenProfile}
                onAccept={() => void handleAcceptChallenge(challenge)}
                onDecline={() => void handleDeclineChallenge(challenge)}
              />
            ))}
          </FriendSection>

          <FriendSection title="Outgoing Challenges" emptyLabel="No pending direct challenges sent to friends.">
            {challengeOverview?.outgoing.map((challenge) => (
              <DirectChallengeCard
                key={challenge.challengeId}
                challenge={challenge}
                busy={busyRequestId === `cancel:${challenge.challengeId}`}
                onOpenProfile={onOpenProfile}
                onCancel={() => void handleCancelChallenge(challenge)}
                readOnly
              />
            ))}
          </FriendSection>

          {accountId && sessionToken ? (
            <div style={{
              marginTop: '4px',
              borderRadius: '14px',
              border: '1px solid rgba(255,180,60,0.12)',
              background: 'rgba(255,255,255,0.02)',
            }}>
              <div style={{ padding: '12px 16px 0', color: '#ffcf72', fontSize: '11px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase' }}>
                Activity
              </div>
              <InboxPage
                embedded
                accountId={accountId}
                sessionToken={sessionToken}
                liveRefreshToken={liveRefreshToken}
                onOpenProfile={onOpenProfile}
                onOpenFriends={onOpenAccount}
                onUnreadCountChange={onUnreadCountChange}
              />
            </div>
          ) : null}
        </div>
      </div>

      <div style={{
        flex: 1,
        minWidth: 0,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'linear-gradient(180deg, rgba(15,18,28,0.98) 0%, rgba(10,12,20,0.96) 100%)',
        border: '1px solid rgba(255,180,60,0.14)',
        borderRadius: '16px',
        boxShadow: '0 18px 52px rgba(0,0,0,0.35)',
        overflow: 'hidden',
      }}>
        <div style={{ padding: '20px 24px 14px', borderBottom: '1px solid rgba(255,180,60,0.12)' }}>
          <div style={{ color: '#ffcf72', fontSize: '13px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase' }}>Accepted Friends</div>
          <div style={{ color: 'rgba(255,232,180,0.72)', fontSize: '12px', marginTop: '4px', lineHeight: 1.5 }}>
            Friends are now the launch foundation for direct account-to-account play. Challenge a friend into a private room without falling back to manual copy-paste lobby links.
          </div>
        </div>

        <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '22px 24px 26px' }}>
          {loading && !overview ? (
            <div style={{ display: 'grid', gap: '14px', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }} aria-busy="true" aria-label="Loading friends">
              {[1, 2, 3].map((i) => (
                <div
                  key={i}
                  style={{
                    height: '140px',
                    borderRadius: '14px',
                    background: 'linear-gradient(90deg, rgba(255,255,255,0.03) 0%, rgba(255,255,255,0.08) 50%, rgba(255,255,255,0.03) 100%)',
                    backgroundSize: '200% 100%',
                    animation: 'shimmer 1.8s infinite',
                    border: '1px solid rgba(255,180,60,0.1)',
                  }}
                />
              ))}
            </div>
          ) : overview?.friends.length ? (
            <div style={{ display: 'grid', gap: '14px', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }}>
              {overview.friends.map((friendship) => (
                <div
                  key={friendship.friendshipId}
                  style={{
                    padding: '16px',
                    borderRadius: '14px',
                    border: '1px solid rgba(255,180,60,0.14)',
                    background: 'rgba(255,255,255,0.03)',
                    display: 'grid',
                    gap: '10px',
                  }}
                >
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: '10px', alignItems: 'center' }}>
                    <div>
                      <div style={{ color: '#fff2c8', fontSize: '15px', fontWeight: 800 }}>@{friendship.account.handle}</div>
                      <div style={{ color: 'rgba(255,232,180,0.64)', fontSize: '12px', marginTop: '3px' }}>
                        Rating {friendship.account.rating ?? 1200} | {describePresence(friendship.account).detail}
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                      <button
                        onClick={() => void handleRemoveFriend(friendship)}
                        disabled={busyRequestId === friendship.friendshipId}
                        style={{
                          minHeight: '36px',
                          padding: '8px 12px',
                          borderRadius: '9px',
                          border: '1px solid rgba(231,76,60,0.32)',
                          background: 'rgba(120,20,20,0.18)',
                          color: '#ffd3ce',
                          fontSize: '11px',
                          fontWeight: 800,
                          cursor: 'pointer',
                          opacity: busyRequestId === friendship.friendshipId ? 0.7 : 1,
                        }}
                      >
                        Remove
                      </button>
                      <button
                        onClick={() => void handleBlockFriend(friendship)}
                        disabled={busyRequestId === `block:${friendship.friendshipId}`}
                        style={{
                          minHeight: '36px',
                          fontSize: '11px',
                          padding: '8px 10px',
                          background: 'rgba(120,20,20,0.28)',
                          color: '#ffd3ce',
                          border: '1px solid rgba(231,76,60,0.25)',
                          borderRadius: '8px',
                          cursor: 'pointer',
                          opacity: busyRequestId === `block:${friendship.friendshipId}` ? 0.6 : 1,
                        }}
                        aria-label={`Block ${friendship.account.handle}`}
                      >
                        Block
                      </button>
                    </div>
                  </div>

                  <div style={{ color: 'rgba(255,232,180,0.78)', fontSize: '12px', lineHeight: 1.6 }}>
                    Friends since {formatDateTime(friendship.createdAt)}
                  </div>
                  <PresencePill account={friendship.account} />

                  <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                    {renderProfileChip(friendship.account.handle)}
                    <button
                      onClick={() => setChallengeTarget(friendship)}
                      disabled={busyRequestId === `challenge:${friendship.friendshipId}`}
                      style={{
                        minHeight: '36px',
                        padding: '8px 14px',
                        borderRadius: '8px',
                        border: '1px solid rgba(86,204,120,0.3)',
                        background: 'rgba(30,110,60,0.2)',
                        color: '#d8ffe5',
                        fontSize: '11px',
                        fontWeight: 800,
                        cursor: 'pointer',
                        opacity: busyRequestId === `challenge:${friendship.friendshipId}` ? 0.7 : 1,
                      }}
                    >
                      Challenge
                    </button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div style={{
              padding: '18px 20px',
              borderRadius: '14px',
              border: '1px dashed rgba(255,180,60,0.18)',
              background: 'rgba(255,255,255,0.02)',
              color: 'rgba(255,232,180,0.72)',
              fontSize: '13px',
              lineHeight: 1.7,
            }}>
              No accepted friends yet. Start by sending a request to another claimed Chess404 handle.
            </div>
          )}
        </div>
      </div>

      {challengeTarget && (
        <ChallengeModal
          target={challengeTarget}
          busy={busyRequestId === `challenge:${challengeTarget.friendshipId}`}
          modeId={challengeModeId}
          onModeChange={setChallengeModeId}
          color={challengeColor}
          onColorChange={setChallengeColor}
          clock={challengeClock}
          onClockChange={setChallengeClock}
          onCancel={() => setChallengeTarget(null)}
          onSend={(modeId, seat, clockSeconds) => void handleSendChallenge(challengeTarget, { modeId, seat, clockSeconds })}
        />
      )}
    </div>
  );
}

function ChallengeModal({
  target,
  busy,
  modeId,
  onModeChange,
  color,
  onColorChange,
  clock,
  onClockChange,
  onCancel,
  onSend,
}: {
  target: FriendshipView;
  busy: boolean;
  modeId: MatchModeId;
  onModeChange: (mode: MatchModeId) => void;
  color: 'black' | 'random' | 'white';
  onColorChange: (color: 'black' | 'random' | 'white') => void;
  clock: ChallengeClockOption;
  onClockChange: (clock: ChallengeClockOption) => void;
  onCancel: () => void;
  onSend: (modeId: MatchModeId, seat: PieceColor, clockSeconds: number) => void;
}): React.ReactElement {
  // Escape closes, like the rest of the app's overlays.
  React.useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onCancel();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [busy, onCancel]);

  const labelStyle: React.CSSProperties = {
    color: 'rgba(255,232,180,0.62)',
    fontSize: '11px',
    fontWeight: 700,
  };
  const controlStyle: React.CSSProperties = {
    minHeight: '42px',
    padding: '10px 12px',
    borderRadius: '10px',
    border: '1px solid rgba(255,180,60,0.22)',
    background: '#121824',
    color: '#fff4d6',
    colorScheme: 'dark',
    fontSize: '12px',
    fontWeight: 700,
    outline: 'none',
    cursor: 'pointer',
  };

  return (
    <div
      onClick={() => { if (!busy) onCancel(); }}
      style={{
        position: 'fixed', inset: 0, zIndex: 1000,
        background: 'rgba(4,6,12,0.72)', backdropFilter: 'blur(3px)',
        display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '18px',
      }}
    >
      <div
        onClick={(event) => event.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={`Challenge ${target.account.handle}`}
        style={{
          width: 'min(420px, 100%)',
          padding: '22px 22px 20px',
          borderRadius: '16px',
          border: '1px solid rgba(255,180,60,0.24)',
          background: 'linear-gradient(180deg, rgba(16,20,32,0.99) 0%, rgba(10,13,22,0.99) 100%)',
          boxShadow: '0 24px 70px rgba(0,0,0,0.55)',
          display: 'grid', gap: '14px',
        }}
      >
        <div style={{ display: 'grid', gap: '4px' }}>
          <div style={{ color: '#ffcf72', fontSize: '11px', fontWeight: 800, letterSpacing: '1.2px', textTransform: 'uppercase' }}>Direct Challenge</div>
          <div style={{ color: '#fff2c8', fontSize: '18px', fontWeight: 800 }}>@{target.account.handle}</div>
        </div>

        <label style={{ display: 'grid', gap: '6px' }}>
          <span style={labelStyle}>Mode</span>
          <select value={modeId} onChange={(event) => onModeChange(event.target.value as MatchModeId)} style={controlStyle}>
            {OFFICIAL_MATCH_MODES.filter((mode) => mode.id !== 'computer').map((mode) => (
              <option key={mode.id} value={mode.id} style={{ background: '#121824', color: '#fff4d6' }}>{mode.label}</option>
            ))}
          </select>
        </label>

        <label style={{ display: 'grid', gap: '6px' }}>
          <span style={labelStyle}>Time control</span>
          <select
            value={clock.seconds}
            onChange={(event) => {
              const seconds = Number(event.target.value);
              const next = CHALLENGE_CLOCK_OPTIONS.find((option) => option.seconds === seconds);
              if (next) onClockChange(next);
            }}
            style={controlStyle}
          >
            {CHALLENGE_CLOCK_OPTIONS.map((option) => (
              <option key={option.seconds} value={option.seconds} style={{ background: '#121824', color: '#fff4d6' }}>{option.label}</option>
            ))}
          </select>
        </label>

        <div style={{ display: 'grid', gap: '6px' }}>
          <span style={labelStyle}>Your color</span>
          <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
            {(['white', 'random', 'black'] as const).map((choice) => (
              <button
                key={choice}
                onClick={() => onColorChange(choice)}
                style={{
                  minHeight: '38px',
                  padding: '8px 16px',
                  borderRadius: '999px',
                  border: color === choice ? '1px solid rgba(255,215,0,0.34)' : '1px solid rgba(255,180,60,0.16)',
                  background: color === choice ? 'rgba(255,180,60,0.16)' : 'rgba(255,255,255,0.03)',
                  color: color === choice ? '#fff2c8' : 'rgba(255,232,180,0.72)',
                  fontSize: '11px',
                  fontWeight: 800,
                  cursor: 'pointer',
                  textTransform: 'capitalize',
                }}
              >
                {choice === 'random' ? '\u2654\u265A Random' : choice}
              </button>
            ))}
          </div>
        </div>

        <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '4px' }}>
          <button
            onClick={onCancel}
            disabled={busy}
            style={{
              minHeight: '40px',
              padding: '10px 16px',
              borderRadius: '10px',
              border: '1px solid rgba(255,255,255,0.12)',
              background: 'rgba(255,255,255,0.05)',
              color: 'rgba(255,232,180,0.82)',
              fontSize: '12px',
              fontWeight: 700,
              cursor: busy ? 'default' : 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            onClick={() => {
              // Random color resolves to a concrete seat at send time; both
              // sides resolving independently is fine -- the challenged
              // player's ACCEPT flow assigns the opposite seat.
              const seat: PieceColor = color === 'random' ? (Math.random() < 0.5 ? 'white' : 'black') : color;
              onSend(modeId, seat, clock.seconds);
            }}
            disabled={busy}
            style={{
              minHeight: '40px',
              padding: '10px 18px',
              borderRadius: '10px',
              border: '1px solid rgba(86,204,120,0.3)',
              background: 'linear-gradient(180deg, rgba(48,140,80,0.42) 0%, rgba(22,84,48,0.5) 100%)',
              color: '#e6ffef',
              fontSize: '12px',
              fontWeight: 800,
              cursor: busy ? 'default' : 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            {busy ? 'Sending\u2026' : 'Send Challenge'}
          </button>
        </div>
      </div>
    </div>
  );
}

function FriendSection({
  title,
  emptyLabel,
  children,
}: React.PropsWithChildren<{ title: string; emptyLabel: string }>): React.ReactElement {
  const items = React.Children.toArray(children);
  return (
    <section style={{ display: 'grid', gap: '10px' }}>
      <div style={{ color: '#fff2c8', fontSize: '12px', fontWeight: 800, letterSpacing: '0.8px', textTransform: 'uppercase' }}>{title}</div>
      {items.length ? items : (
        <div style={{ padding: '12px 14px', borderRadius: '10px', background: 'rgba(255,255,255,0.03)', border: '1px dashed rgba(255,180,60,0.12)', color: 'rgba(255,232,180,0.66)', fontSize: '12px', lineHeight: 1.6 }}>
          {emptyLabel}
        </div>
      )}
    </section>
  );
}

function FriendRequestCard({
  request,
  busy,
  onOpenProfile,
  onAccept,
  onDecline,
  readOnly = false,
}: {
  request: FriendRequestView;
  busy: boolean;
  onOpenProfile?: (handle: string) => void;
  onAccept?: () => void;
  onDecline?: () => void;
  readOnly?: boolean;
}): React.ReactElement {
  return (
    <div style={{ padding: '14px', borderRadius: '12px', border: '1px solid rgba(255,180,60,0.12)', background: 'rgba(255,255,255,0.03)', display: 'grid', gap: '9px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '10px', alignItems: 'center' }}>
        <div>
          <div style={{ color: '#fff2c8', fontSize: '14px', fontWeight: 800 }}>@{request.account.handle}</div>
          <div style={{ color: 'rgba(255,232,180,0.64)', fontSize: '11px', marginTop: '3px' }}>
            {request.account.rating ?? 1200} rating | {describePresence(request.account).detail}
          </div>
        </div>
        <button
          onClick={() => onOpenProfile?.(request.account.handle)}
          style={{
            padding: '7px 10px',
            borderRadius: '8px',
            border: '1px solid rgba(255,180,60,0.18)',
            background: 'rgba(255,180,60,0.06)',
            color: '#ffe7a9',
            fontSize: '11px',
            fontWeight: 800,
            cursor: 'pointer',
          }}
        >
          Profile
        </button>
      </div>
      <PresencePill account={request.account} />
      {!readOnly ? (
        <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          <button
            onClick={onAccept}
            disabled={busy}
            style={{
              padding: '8px 10px',
              borderRadius: '9px',
              border: '1px solid rgba(86,204,120,0.32)',
              background: 'rgba(30,110,60,0.2)',
              color: '#d8ffe5',
              fontSize: '11px',
              fontWeight: 800,
              cursor: 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            Accept
          </button>
          <button
            onClick={onDecline}
            disabled={busy}
            style={{
              padding: '8px 10px',
              borderRadius: '9px',
              border: '1px solid rgba(231,76,60,0.26)',
              background: 'rgba(120,20,20,0.18)',
              color: '#ffd3ce',
              fontSize: '11px',
              fontWeight: 800,
              cursor: 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            Decline
          </button>
        </div>
      ) : (
        <div style={{ color: 'rgba(255,232,180,0.62)', fontSize: '11px' }}>
          Pending since {formatDateTime(request.createdAt)}
        </div>
      )}
    </div>
  );
}

function DirectChallengeCard({
  challenge,
  busy,
  onOpenProfile,
  onAccept,
  onDecline,
  onCancel,
  readOnly = false,
}: {
  challenge: DirectChallengeView;
  busy: boolean;
  onOpenProfile?: (handle: string) => void;
  onAccept?: () => void;
  onDecline?: () => void;
  onCancel?: () => void;
  readOnly?: boolean;
}): React.ReactElement {
  const seatText = challenge.viewerSeat
    ? `You play ${challenge.viewerSeat}`
    : challenge.challengerSeat
      ? `Challenger prefers ${challenge.challengerSeat}`
      : 'Seat assigned on join';

  return (
    <div style={{ padding: '14px', borderRadius: '12px', border: '1px solid rgba(255,180,60,0.12)', background: 'rgba(255,255,255,0.03)', display: 'grid', gap: '9px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '10px', alignItems: 'center' }}>
        <div>
          <div style={{ color: '#fff2c8', fontSize: '14px', fontWeight: 800 }}>@{challenge.account.handle}</div>
          <div style={{ color: 'rgba(255,232,180,0.64)', fontSize: '11px', marginTop: '3px', lineHeight: 1.5 }}>
            {modeLabel(challenge.modeId)} | {seatText} | {describePresence(challenge.account).detail}
          </div>
        </div>
        <button
          onClick={() => onOpenProfile?.(challenge.account.handle)}
          style={{
            padding: '7px 10px',
            borderRadius: '8px',
            border: '1px solid rgba(255,180,60,0.18)',
            background: 'rgba(255,180,60,0.06)',
            color: '#ffe7a9',
            fontSize: '11px',
            fontWeight: 800,
            cursor: 'pointer',
          }}
        >
          Profile
        </button>
      </div>
      <PresencePill account={challenge.account} />
      <div style={{ color: 'rgba(255,232,180,0.72)', fontSize: '11px', lineHeight: 1.6 }}>
        Match room {challenge.matchId} | Created {formatDateTime(challenge.createdAt)}
      </div>
      {!readOnly ? (
        <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          <button
            onClick={onAccept}
            disabled={busy}
            style={{
              padding: '8px 10px',
              borderRadius: '9px',
              border: '1px solid rgba(86,204,120,0.32)',
              background: 'rgba(30,110,60,0.2)',
              color: '#d8ffe5',
              fontSize: '11px',
              fontWeight: 800,
              cursor: 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            Accept Challenge
          </button>
          <button
            onClick={onDecline}
            disabled={busy}
            style={{
              padding: '8px 10px',
              borderRadius: '9px',
              border: '1px solid rgba(231,76,60,0.26)',
              background: 'rgba(120,20,20,0.18)',
              color: '#ffd3ce',
              fontSize: '11px',
              fontWeight: 800,
              cursor: 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            Decline
          </button>
        </div>
      ) : (
        <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          <div style={{ color: 'rgba(255,232,180,0.62)', fontSize: '11px', alignSelf: 'center' }}>
            Waiting for your friend to accept.
          </div>
          <button
            onClick={onCancel}
            disabled={busy}
            style={{
              padding: '8px 10px',
              borderRadius: '9px',
              border: '1px solid rgba(231,76,60,0.26)',
              background: 'rgba(120,20,20,0.18)',
              color: '#ffd3ce',
              fontSize: '11px',
              fontWeight: 800,
              cursor: 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            Cancel Challenge
          </button>
        </div>
      )}
    </div>
  );
}

function PresencePill({ account }: { account: AccountProfile }): React.ReactElement {
  const presence = describePresence(account);
  const isOnline = account.presenceStatus === 'online';
  const isRecent = account.presenceStatus === 'recently_active';
  return (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '6px 9px',
        borderRadius: '999px',
        border: presence.border,
        background: presence.background,
        color: presence.color,
        fontSize: '10px',
        fontWeight: 800,
        letterSpacing: '0.5px',
        textTransform: 'uppercase',
      }}
    >
      <span style={{
        width: '7px',
        height: '7px',
        borderRadius: '999px',
        background: isOnline ? '#7cff9c' : isRecent ? '#ffd36f' : 'rgba(255,255,255,0.35)',
        boxShadow: isOnline ? '0 0 12px rgba(124,255,156,0.65)' : 'none',
      }} />
      {presence.label}
    </div>
  );
}
