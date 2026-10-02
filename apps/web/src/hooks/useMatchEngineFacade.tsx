'use client';

import React from 'react';
import { useRouter } from 'next/navigation';
import type {
  MatchModeId,
  MatchSnapshotMessage,
  PlayerIntent,
} from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
import { classifySnapshotTier, filterUnseenEventIds } from '../lib/snapshot-tier';
import { useStockfish } from '../usestockfish';
import type {
  Board,
  PieceType,
  PieceColor,
  Piece,
  Sq,
  GameCard,
  CardPendingState,
  DoubleMove,
  FogZone,
  FortressZone,
  Snapshot,
  CardMechanic,
} from '../types';
import {
  makeBoard,
  findKing,
  isAttackedWithFusion,
} from '../chessEngine';
import {
  OPP,
  FILES,
  RANKS,
} from '../constants';
import {
  applyIntent,
  fetchMatch,
  isMatchGone,
  markMatchGone,
  readStoredRoomMeta,
  resolveSeatSecret,
  type StoredRoomMeta,
  writeStoredRoomMeta,
} from '../lib/match-service';
import { joinPrivateMatch, rematchPrivateMatch } from '../lib/private-match-service';
import { buildPendingCardFromSnapshot } from '../lib/pending-card-from-snapshot';
import {
  claimMatchSeat,
  type GuestProfile,
} from '../lib/platform-service';
import type { QueueName, QueueTicket } from '../lib/matchmaking-service';
import {
  writeStoredActiveMatchId,
  readStoredAccountIdentity,
  readStoredGuestIdentity,
  clearRequestedMatchQuery,
  buildLiveMatchUrl,
  buildReplayPageUrl,
  copyTextToClipboard,
} from '../lib/session-storage';
import {
  type SocialAlert,
} from '../lib/match-labels';
import { useMatchTimer } from './useMatchTimer';
import { useMatchReplay } from './useMatchReplay';
import { usePlatformState } from './usePlatformState';

function buildMoveRows(history: string[]): { n: string; w?: string; b?: string }[] {
  const rows: { n: string; w?: string; b?: string }[] = [];
  for (let i = 0; i < history.length; i += 2) {
    rows.push({ n: `${Math.floor(i / 2) + 1}.`, w: history[i], b: history[i + 1] });
  }
  return rows;
}
import { useMatchConnection } from './useMatchConnection';
import { useBoardInteraction } from './useBoardInteraction';
import { useMatchNav } from './useMatchNav';
import { useMatchAnimations } from './useMatchAnimations';
import { useCardEngine } from './useCardEngine';
import { useMatchChat } from './useMatchChat';
import { useMatchAntiCheat } from './useMatchAntiCheat';
import { useMatchBoardEffects } from './useMatchBoardEffects';
import { useSound, playSound } from './useSound';
import { useCardInteraction, AUTHORITATIVE_JOKER_MECHANICS } from './useCardInteraction';
import { useBoardMoveHandler } from './useBoardMoveHandler';
import { useMatchUIHelpers } from './useMatchUIHelpers';

type AppPage =
  | 'Play'
  | 'Match'
  | 'Watch'
  | 'Rankings'
  | 'Profiles'
  | 'Account'
  | 'History'
  | 'Friends'
  | 'Inbox'
  | 'Cards'
  | 'Community'
  | 'Status'
  | 'Admin'
  | 'Queue';

function buildStoredRoomMeta(
  base: StoredRoomMeta | null | undefined,
  whiteProfile: GuestProfile | null,
  blackProfile: GuestProfile | null,
  whiteSessionSecret: string | null,
  blackSessionSecret: string | null,
  options: { ensureSecrets?: boolean } = {},
): StoredRoomMeta {
  return {
    ...base,
    modeId: base?.modeId ?? DEFAULT_MATCH_MODE_ID,
    whiteGuestId: base?.whiteGuestId ?? whiteProfile?.guestId,
    blackGuestId: base?.blackGuestId ?? blackProfile?.guestId,
    whiteAccountId: base?.whiteAccountId ?? readStoredAccountIdentity('white').accountId,
    blackAccountId: base?.blackAccountId ?? readStoredAccountIdentity('black').accountId,
    whiteName: base?.whiteName ?? whiteProfile?.displayName,
    blackName: base?.blackName ?? blackProfile?.displayName,
    whitePlayerSecret: options.ensureSecrets ? resolveSeatSecret(base?.whitePlayerSecret, whiteSessionSecret) : base?.whitePlayerSecret,
    blackPlayerSecret: options.ensureSecrets ? resolveSeatSecret(base?.blackPlayerSecret, blackSessionSecret) : base?.blackPlayerSecret,
  };
}

export interface UseMatchEngineProps {
  accountActionQueryDetected: boolean;
  activePage: AppPage;
  authoritativeRematchBusy: boolean;
  blackProfile: GuestProfile | null;
  communityFocusGuestId: string | null;
  friendsAttentionCount: number;
  guestProfilesReady: boolean;
  historyFocusGuestId: string | null;
  historyFocusMatchId: string | null;
  historyQueryReady: boolean;
  hostedRuntime: boolean | null;
  inboxUnreadCount: number;
  matchDestinationNotice: string;
  matchQueryReady: boolean;
  matchSeatMeta: {
    whiteGuestId?: string;
    blackGuestId?: string;
    whiteName?: string;
    blackName?: string;
  } | null;
  openedBoardMatchRef: React.MutableRefObject<string | null>;
  pathname: string;
  profileFocusHandle: string | null;
  profileQueryReady: boolean;
  bootstrapQueueRecovery: {
    white: QueueTicket | null;
    black: QueueTicket | null;
  } | null;
  queueLaunchIntent: { modeId: MatchModeId; queue: QueueName } | null;
  router: ReturnType<typeof useRouter>;
  socialAlert: SocialAlert | null;
  socialLiveToken: number;
  viewerSeat: PieceColor | null;
  whiteProfile: GuestProfile | null;

  setAccountActionQueryDetected: React.Dispatch<React.SetStateAction<boolean>>;
  setActivePage: React.Dispatch<React.SetStateAction<AppPage>>;
  setAuthoritativeRematchBusy: React.Dispatch<React.SetStateAction<boolean>>;
  setBlackProfile: React.Dispatch<React.SetStateAction<GuestProfile | null>>;
  setFriendsAttentionCount: React.Dispatch<React.SetStateAction<number>>;
  setGuestProfilesReady: React.Dispatch<React.SetStateAction<boolean>>;
  setHistoryFocusGuestId: React.Dispatch<React.SetStateAction<string | null>>;
  setHistoryFocusMatchId: React.Dispatch<React.SetStateAction<string | null>>;
  setHistoryQueryReady: React.Dispatch<React.SetStateAction<boolean>>;
  setHostedRuntime: React.Dispatch<React.SetStateAction<boolean | null>>;
  setInboxUnreadCount: React.Dispatch<React.SetStateAction<number>>;
  setMatchDestinationNotice: React.Dispatch<React.SetStateAction<string>>;
  setMatchQueryReady: React.Dispatch<React.SetStateAction<boolean>>;
  setMatchSeatMeta: React.Dispatch<React.SetStateAction<{
    whiteGuestId?: string;
    blackGuestId?: string;
    whiteAccountId?: string;
    blackAccountId?: string;
    whiteName?: string;
    blackName?: string;
  } | null>>;
  setProfileFocusHandle: React.Dispatch<React.SetStateAction<string | null>>;
  setProfileQueryReady: React.Dispatch<React.SetStateAction<boolean>>;
  setBootstrapQueueRecovery: React.Dispatch<React.SetStateAction<{
    white: QueueTicket | null;
    black: QueueTicket | null;
  } | null>>;
  setCommunityFocusGuestId: React.Dispatch<React.SetStateAction<string | null>>;
  setQueueLaunchIntent: React.Dispatch<React.SetStateAction<{ modeId: MatchModeId; queue: QueueName } | null>>;
  setSecondaryMenuOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setSocialAlert: React.Dispatch<React.SetStateAction<SocialAlert | null>>;
  setSocialLiveToken: React.Dispatch<React.SetStateAction<number>>;
  setViewerSeat: React.Dispatch<React.SetStateAction<PieceColor | null>>;
  setWhiteProfile: React.Dispatch<React.SetStateAction<GuestProfile | null>>;
}

export function useMatchEngineFacade(props: UseMatchEngineProps) {
  const {
    accountActionQueryDetected, activePage, authoritativeRematchBusy, blackProfile, communityFocusGuestId,
    friendsAttentionCount, guestProfilesReady, historyFocusGuestId, historyFocusMatchId, historyQueryReady,
    hostedRuntime, inboxUnreadCount, matchDestinationNotice, matchQueryReady, matchSeatMeta,
    openedBoardMatchRef, pathname, profileFocusHandle, profileQueryReady, queueLaunchIntent, router,
    setAccountActionQueryDetected, setActivePage, setAuthoritativeRematchBusy, setBlackProfile,
    setFriendsAttentionCount, setGuestProfilesReady, setHistoryFocusGuestId, setHistoryFocusMatchId,
    setHistoryQueryReady, setHostedRuntime, setInboxUnreadCount, setMatchDestinationNotice,
    setMatchQueryReady, setMatchSeatMeta, setProfileFocusHandle, setProfileQueryReady,
    setBootstrapQueueRecovery, setQueueLaunchIntent, setSecondaryMenuOpen, setSocialAlert,
    setSocialLiveToken, setViewerSeat, setWhiteProfile, socialAlert, socialLiveToken, viewerSeat, whiteProfile
  } = props;

  const platformState = usePlatformState({
    hostedRuntime, setHostedRuntime,
    activePage, setActivePage,
    setAccountActionQueryDetected,
    setHistoryFocusMatchId,
    setHistoryFocusGuestId,
    setProfileFocusHandle,
    setProfileQueryReady, setHistoryQueryReady, setMatchQueryReady,
    setFriendsAttentionCount, setInboxUnreadCount,
    setSocialAlert, socialAlert,
    setSocialLiveToken, socialLiveToken,
    setWhiteProfile, setBlackProfile, setViewerSeat, viewerSeat,
    whiteProfile, blackProfile,
    setGuestProfilesReady, guestProfilesReady,
    setBootstrapQueueRecovery,
    setMatchSeatMeta,
    setMatchDestinationNotice,
    openedBoardMatchRef,
    pathname,
    profileFocusHandle,
    historyFocusGuestId, historyFocusMatchId,
  });

  const {
    primaryAccountIdentity, setPrimaryAccountIdentity, shellAccountNotice, setShellAccountNotice,
    whiteProfileRef, blackProfileRef, viewerSeatRef, guestSessionSecretsRef, authoritativeSeatIdsRef,
    authoritativeSeatSecretsRef, authoritativeClaimExpiresAtRef, authoritativeClaimTokensRef,
    gatewayBootstrapClaimsRef, gatewayRecoveredMatchIdRef, requestedMatchIdRef, authoritativeMatchIdRef,
    dismissedSocialAlertIdsRef, intentInFlight, setIntentInFlight, syncPrimaryAccountIdentity,
    clearPrimaryAccountRestriction, pulseSocialLive, handleSeatAuthenticated, handlePrimaryShellAuthenticated,
    applyGatewayGuestSessions, applyGatewayAccountSessions, buildGatewayBootstrapRequest,
    applyGatewayMatchClaims, applyGatewayRecoveredMatch, applyGatewayQueueRecovery,
  } = platformState;

  const {
    board, setBoard, turn, setTurn, sel, setSel, hints, setHints, premove, setPremove,
    moved, setMoved, lm, setLm, drag, setDrag, dragPos, setDragPos, promo, setPromo,
    check, setCheck, mate, setMate, stale, setStale, insuf, setInsuf, hmc, setHmc,
    fmn, setFmn, posHist, setPosHist, drawOffer, setDrawOffer, over, setOver, winner, setWinner,
    authoritativeFinishReason, setAuthoritativeFinishReason, movHist, setMovHist, snapshots, setSnapshots,
    analysisArrows, setAnalysisArrows, boardRef, turnRef, movedRef, lmRef, hmcRef, fmnRef, posHistRef,
    overRef, premoveRef,
  } = useBoardInteraction();

  const openProfileHandle = React.useCallback((handle: string) => {
    const normalized = handle.trim().toLowerCase();
    if (!normalized) return;
    setProfileFocusHandle(normalized);
    router.push('/profiles');
  }, [router, setProfileFocusHandle]);

  const openReplayMatch = React.useCallback((matchId: string, guestId: string | null = null) => {
    const normalizedMatchId = matchId.trim();
    if (!normalizedMatchId) return;
    setHistoryFocusMatchId(normalizedMatchId);
    setHistoryFocusGuestId(guestId);
    router.push('/history');
  }, [router, setHistoryFocusGuestId, setHistoryFocusMatchId]);

  const openGuestHistory = React.useCallback((guestId: string) => {
    const normalizedGuestId = guestId.trim();
    if (!normalizedGuestId) return;
    setHistoryFocusGuestId(normalizedGuestId);
    setHistoryFocusMatchId(null);
    router.push('/history');
  }, [router, setHistoryFocusGuestId, setHistoryFocusMatchId]);

  const openLiveMatch = React.useCallback((matchId: string) => {
    const normalizedMatchId = matchId.trim();
    if (!normalizedMatchId) return;
    if (activePage !== 'Match') {
      setActivePage('Match');
    }
    const url = buildLiveMatchUrl(normalizedMatchId);
    if (url) router.push(url);
  }, [activePage, router, setActivePage]);

  const copyLiveMatchLink = React.useCallback(async (matchId: string) => {
    const normalizedMatchId = matchId.trim();
    if (!normalizedMatchId) return;
    const url = `${window.location.origin}${buildLiveMatchUrl(normalizedMatchId)}`;
    await copyTextToClipboard(url);
    // SocialAlert has no toast-style fields — skip setting it for a simple copy action
  }, [setSocialAlert]);

  const copyReplayPageLink = React.useCallback(async (matchId: string) => {
    const normalizedMatchId = matchId.trim();
    if (!normalizedMatchId) return;
    const url = `${window.location.origin}${buildReplayPageUrl(normalizedMatchId)}`;
    await copyTextToClipboard(url);
    // SocialAlert has no toast-style fields — skip setting it for a simple copy action
  }, [setSocialAlert]);

  const [engineOn, setEngineOn] = React.useState(false);
  const { isReady: sfReady, isThinking, ev, sfErr, analyse, stop, resetEval } = useStockfish(engineOn);

  const {
    timeW, setTimeW, timeB, setTimeB, tickingState, setTicking, clockActive, setClockActive,
    resetTimer,
  } = useMatchTimer();

  const {
    reviewIdx, setReviewIdx, reviewBoard, setReviewBoard, isReviewing,
    goToSnap, reviewFirst, reviewPrev, reviewNext, reviewLast,
  } = useMatchReplay({ snapshots, over, resetEval });

  const animations = useMatchAnimations();
  const {
    cardAnim, setCardAnim, cardAnimLbl, setCardAnimLbl, fireCardAnim,
    bombPieces, setBombPieces, bombExploding, setBombExploding, bombPiecesRef,
    swapAnim, setSwapAnim, transformAnim, setTransformAnim, triggerTransformAnim,
    sniperAnim, setSniperAnim, triggerSniperAnim, teleportAnim, setTeleportAnim,
    triggerTeleportAnim, jumpAnim, setJumpAnim, triggerJumpAnim, sacrificeAnim,
    setSacrificeAnim, triggerSacrificeAnim, mindControlAnim, setMindControlAnim,
    triggerMindControlAnim, fuseAnim, setFuseAnim, triggerFuseAnim,
    reverseAnim, setReverseAnim, triggerReverseAnim,
    cloneAnim, setCloneAnim, triggerCloneAnim,
    blackHoleAnim, setBlackHoleAnim, triggerBlackHoleAnim,
    poofAnim, setPoofAnim, triggerPoofAnim,
    triggerSwapAnim,
  } = animations;

  const [doubleMove, setDoubleMove] = React.useState<DoubleMove | null>(null);
  const doubleMoveRef = React.useRef<DoubleMove | null>(null);

  const {
    lavaSquares, setLavaSquares, lavaExploding, setLavaExploding,
    ghostPiece, setGhostPiece, ghostRef, fogZones, setFogZones,
    handleLavaLanding,
  } = useMatchBoardEffects({
    setBoard,
    setCardMsg: (msg: string) => {},
    fireCardAnim,
    bombPiecesRef,
    setBombPieces,
    setBombExploding,
  });

  const [fortressZones, setFortressZones] = React.useState<FortressZone[]>([]);
  const [radarActive, setRadarActive] = React.useState(false);

  const playMoveSound = React.useCallback(() => playSound('move'), []);
  // Mechanic-aware: optional for now (sound pool has one card sound); kept as a
  // param so per-card audio can land without touching call sites again.
  const playCardSound = React.useCallback((_mechanic?: string) => playSound('card_play'), []);
  const { chatMessages, setChatMessages, chatInput, setChatInput, chatRef, resetChat } = useMatchChat();
  const { resetAntiCheat } = useMatchAntiCheat();

  const {
    whiteHand, setWhiteHand, blackHand, setBlackHand, selectedCard, setSelectedCard,
    dealPhase, setDealPhase, lastDrawAnim, setLastDrawAnim, cardPending, setCardPending,
    cardMsg, setCardMsg, promoPicker, setPromoPicker, cardPromo, setCardPromo,
    cardUsedBy, setCardUsedBy, jokerPicker, setJokerPicker, pendingCardUseRef,
    cardUsedByRef, resetCardUsed, removeCardFromHand, finishCardUse,
  } = useCardEngine(
    board, turn, moved, lm, fmn, fmnRef, boardRef, turnRef,
    authoritativeMatchIdRef.current, hostedRuntime, viewerSeatRef
  );

  const jokerRef = React.useRef<HTMLDivElement>(null);
  const blackMovedRef = React.useRef(false);
  const finalPositionRef = React.useRef<{ fen: string; turn: PieceColor } | null>(null);
  const [gameKey, setGameKey] = React.useState(0);

  const [authoritativeLive, setAuthoritativeLive] = React.useState(false);
  const [authoritativeMatchId, setAuthoritativeMatchId] = React.useState<string | null>(null);
  const [matchLoadError, setMatchLoadError] = React.useState<string | null>(null);
  // A rated private invite only accepts signed-in joiners (gateway 403s the
  // join otherwise). Latched here so the shell can render a dedicated
  // "create an account to enter this match" screen with an auto-retry once
  // the visitor authenticates, instead of a dead-end access error.
  const [ratedInviteSignInRequired, setRatedInviteSignInRequired] = React.useState(false);
  // Match IDs the server has definitively declared gone (finished/archived,
  // 404/410) in this page session. Without the latch, every navigation to a
  // stale /match/<id> (or any page while requestedMatchIdRef still points at
  // one) re-ran the bootstrap chain and re-logged "match is not public".
  // Ref, not state: it must not retrigger effects, only gate them.
  const goneMatchIdsRef = React.useRef<Set<string>>(new Set());
  const [authoritativeStatus, setAuthoritativeStatus] = React.useState<'waiting' | 'active' | 'finished' | null>(null);
  const [authoritativeWhiteConnected, setAuthoritativeWhiteConnected] = React.useState(false);
  const [authoritativeBlackConnected, setAuthoritativeBlackConnected] = React.useState(false);
  const [authoritativeDisconnectGraceFor, setAuthoritativeDisconnectGraceFor] = React.useState<PieceColor | null>(null);
  const [authoritativeDisconnectGraceDeadline, setAuthoritativeDisconnectGraceDeadline] = React.useState<string | null>(null);
  const [streamDisconnected, setStreamDisconnected] = React.useState(false);

  const authoritativeActorForColor = React.useCallback((color: PieceColor): { playerId: string; playerSecret?: string; playerClaimToken?: string } => {
    const seatId = authoritativeSeatIdsRef.current[color];
    const seatSecret = authoritativeSeatSecretsRef.current[color];
    const claimToken = authoritativeClaimTokensRef.current[color];
    const claimExpiresAt = authoritativeClaimExpiresAtRef.current[color];
    const tokenValid = !!claimToken && (!claimExpiresAt || new Date(claimExpiresAt).getTime() > Date.now());
    return {
      // A hosted action must carry the server-owned guest ID.  Falling back to
      // the display colour here turns a hydration race into a malformed action
      // such as playerId="white", which the match service cannot authorize.
      playerId: seatId || (hostedRuntime ? '' : color),
      playerSecret: seatSecret || undefined,
      playerClaimToken: tokenValid ? claimToken : undefined,
    };
  }, [authoritativeClaimExpiresAtRef, authoritativeClaimTokensRef, authoritativeSeatIdsRef, authoritativeSeatSecretsRef, hostedRuntime]);

  // Strictly monotonic snapshot application (bounce fix) via the shared
  // classifySnapshotTier helper -- see lib/snapshot-tier.ts for the tier
  // rationale (equal-seq clock ticks must stay cosmetic, not dropped).
  const appliedSeqRef = React.useRef(0);
  const appliedEventIdsRef = React.useRef<Set<string>>(new Set());

  const applyAuthoritativeSnapshot = React.useCallback((snapshot: MatchSnapshotMessage) => {
    const match = snapshot.match;
    if (!match) return;

    const tier = classifySnapshotTier(snapshot.seqNum, appliedSeqRef.current);
    if (tier === 'stale') return;
    const cosmeticOnly = tier === 'cosmetic';
    if (tier === 'fresh' && (snapshot.seqNum ?? 0) > 0) {
      appliedSeqRef.current = snapshot.seqNum as number;
    }

    if (cosmeticOnly) {
      // Same-seq frame: the clock tick. Update only what it legitimately
      // carries -- clocks and (defensively) terminal state. Never board,
      // turn, hands, pending cards or identity, and never event side effects.
      if (match.clock) {
        setTimeW(match.clock.whiteMs);
        setTimeB(match.clock.blackMs);
      }
      if (match.status === 'finished') {
        setOver(true);
        setWinner(match.winner ?? null);
      }
      return;
    }

    const freshEvents = filterUnseenEventIds(snapshot.events, appliedEventIdsRef.current) as NonNullable<typeof snapshot.events>;

    // Snapshot hydration is the boundary between a public match URL and an
    // authenticated player action.  The façade used to update only the board,
    // leaving the authoritative actor refs empty.  A fast first click then
    // submitted a fabricated colour as playerId.  Keep the exact seat ID and
    // credential that were issued for this room together before enabling play.
    const storedRoomMeta = readStoredRoomMeta(match.matchId);
    authoritativeSeatIdsRef.current = {
      white: match.whiteGuestId ?? null,
      black: match.blackGuestId ?? null,
    };
    authoritativeSeatSecretsRef.current = {
      white: storedRoomMeta?.whitePlayerSecret ?? authoritativeSeatSecretsRef.current.white,
      black: storedRoomMeta?.blackPlayerSecret ?? authoritativeSeatSecretsRef.current.black,
    };
    authoritativeClaimTokensRef.current = {
      white: storedRoomMeta?.whiteClaimToken ?? authoritativeClaimTokensRef.current.white,
      black: storedRoomMeta?.blackClaimToken ?? authoritativeClaimTokensRef.current.black,
    };
    authoritativeClaimExpiresAtRef.current = {
      white: storedRoomMeta?.whiteClaimExpiresAt ?? authoritativeClaimExpiresAtRef.current.white,
      black: storedRoomMeta?.blackClaimExpiresAt ?? authoritativeClaimExpiresAtRef.current.black,
    };

    const localWhiteGuestID = whiteProfileRef.current?.guestId ?? readStoredGuestIdentity('white').guestId ?? null;
    const localBlackGuestID = blackProfileRef.current?.guestId ?? readStoredGuestIdentity('black').guestId ?? null;
    let derivedViewerSeat: PieceColor | null = null;
    if (hostedRuntime) {
      if (localWhiteGuestID && match.whiteGuestId === localWhiteGuestID) {
        derivedViewerSeat = 'white';
      } else if (localBlackGuestID && match.blackGuestId === localBlackGuestID) {
        derivedViewerSeat = 'black';
      } else {
        derivedViewerSeat = storedRoomMeta?.viewerSeat ?? null;
      }
    } else {
      derivedViewerSeat = 'white';
    }
    viewerSeatRef.current = derivedViewerSeat;
    setViewerSeat(derivedViewerSeat);

    const nextRoomMeta: StoredRoomMeta = {
      ...storedRoomMeta,
      queue: match.queue ?? storedRoomMeta?.queue,
      modeId: match.modeId ?? storedRoomMeta?.modeId ?? DEFAULT_MATCH_MODE_ID,
      difficulty: storedRoomMeta?.difficulty,
      viewerSeat: derivedViewerSeat,
      whiteGuestId: match.whiteGuestId ?? storedRoomMeta?.whiteGuestId,
      blackGuestId: match.blackGuestId ?? storedRoomMeta?.blackGuestId,
      whiteAccountId: match.whiteAccountId ?? storedRoomMeta?.whiteAccountId,
      blackAccountId: match.blackAccountId ?? storedRoomMeta?.blackAccountId,
      whiteName: match.whiteName ?? storedRoomMeta?.whiteName,
      blackName: match.blackName ?? storedRoomMeta?.blackName,
      whitePlayerSecret: authoritativeSeatSecretsRef.current.white ?? undefined,
      blackPlayerSecret: authoritativeSeatSecretsRef.current.black ?? undefined,
      whiteClaimToken: authoritativeClaimTokensRef.current.white ?? undefined,
      blackClaimToken: authoritativeClaimTokensRef.current.black ?? undefined,
      whiteClaimExpiresAt: authoritativeClaimExpiresAtRef.current.white ?? undefined,
      blackClaimExpiresAt: authoritativeClaimExpiresAtRef.current.black ?? undefined,
    };
    // A browser must never retain the other hosted player's secret merely
    // because it has loaded the same public room URL.
    if (hostedRuntime && derivedViewerSeat === 'white') {
      delete nextRoomMeta.blackPlayerSecret;
      delete nextRoomMeta.blackClaimToken;
      delete nextRoomMeta.blackClaimExpiresAt;
    } else if (hostedRuntime && derivedViewerSeat === 'black') {
      delete nextRoomMeta.whitePlayerSecret;
      delete nextRoomMeta.whiteClaimToken;
      delete nextRoomMeta.whiteClaimExpiresAt;
    }
    writeStoredRoomMeta(match.matchId, nextRoomMeta);
    writeStoredActiveMatchId(match.matchId);
    setMatchSeatMeta({
      whiteGuestId: nextRoomMeta.whiteGuestId,
      blackGuestId: nextRoomMeta.blackGuestId,
      whiteAccountId: nextRoomMeta.whiteAccountId,
      blackAccountId: nextRoomMeta.blackAccountId,
      whiteName: nextRoomMeta.whiteName,
      blackName: nextRoomMeta.blackName,
    });

    setAuthoritativeMatchId(match.matchId);
    authoritativeMatchIdRef.current = match.matchId;
    setAuthoritativeLive(match.status === 'active' || match.status === 'waiting');
    setAuthoritativeStatus(match.status);
    setAuthoritativeFinishReason(match.finishReason ?? null);
    setAuthoritativeWhiteConnected(match.whiteConnected);
    setAuthoritativeBlackConnected(match.blackConnected);
    setAuthoritativeDisconnectGraceFor(match.disconnectGraceFor ?? null);
    setAuthoritativeDisconnectGraceDeadline(match.disconnectGraceDeadline ?? null);

    boardRef.current = match.board as Board;
    turnRef.current = match.turn as PieceColor;
    movedRef.current = new Set(match.moved);
    lmRef.current = match.lastMove;
    hmcRef.current = match.halfMoveClock;
    fmnRef.current = match.fullMoveNumber;

    setBoard(match.board as Board);
    const isNewMatch = authoritativeMatchIdRef.current !== match.matchId;
    if (isNewMatch) {
      resetCardUsed('white');
      resetCardUsed('black');
    } else if (turnRef.current !== match.turn) {
      resetCardUsed(match.turn as PieceColor);
    }
    // The server is the source of truth for the one-card-per-turn slot
    // (cardUsedThisTurn mirrors what removeCardFromHand consumed). Seed the
    // local cardUsedBy flags from it so a mid-turn reload -- or a snapshot
    // that arrives after a local optimistic play -- shows honest UI state
    // instead of offering a card the server will refuse.
    const serverCardUsed = match.cardUsedThisTurn ?? null;
    if (serverCardUsed) {
      const nextFlags = {
        white: serverCardUsed.white === true,
        black: serverCardUsed.black === true,
      };
      if (
        cardUsedByRef.current.white !== nextFlags.white ||
        cardUsedByRef.current.black !== nextFlags.black
      ) {
        cardUsedByRef.current = nextFlags;
        setCardUsedBy(nextFlags);
      }
    } else if (isNewMatch) {
      cardUsedByRef.current = { white: false, black: false };
      setCardUsedBy({ white: false, black: false });
    }
    setTurn(match.turn);
    setMoved(new Set(match.moved));
    setLm(match.lastMove);
    setHmc(match.halfMoveClock);
    setFmn(match.fullMoveNumber);
    if (match.moveHistory) {
      setMovHist(buildMoveRows(match.moveHistory));
    }
    // Sync the chat log from the authoritative snapshot. The facade applier
    // never mapped match.chatMessages -- only the other runtime did -- so in
    // hosted matches the server accepted every message and NOBODY ever saw
    // it, not even the sender (no local echo either). This is the actual
    // "chat is broken" root cause; the swallowed send errors were only the
    // second half.
    if (Array.isArray(match.chatMessages)) {
      setChatMessages(match.chatMessages.map(msg => ({ sender: msg.sender as 'white' | 'black', text: msg.text })));
    }

    if (snapshot.events && freshEvents.length > 0) {
      const mySeat = viewerSeatRef.current;
      const myActor = mySeat ? authoritativeActorForColor(mySeat) : null;
      for (const ev of freshEvents) {
        if (ev.type === 'move_applied') {
          if (ev.payload?.lavaTriggered && ev.payload?.to) {
            const lavaSq = ev.payload.to as Sq;
            setLavaExploding([lavaSq]);
            setTimeout(() => setLavaExploding([]), 1400);
            playSound('capture');
            setCardMsg(`🌋 Lava trap consumed piece at ${FILES[lavaSq.col]}${RANKS[lavaSq.row]}!`);
          }
          if (Array.isArray(ev.payload?.bombExplodedSquares) && ev.payload.bombExplodedSquares.length > 0) {
            const bSqs = ev.payload.bombExplodedSquares as Sq[];
            setBombExploding(bSqs);
            setTimeout(() => setBombExploding([]), 1600);
            playSound('capture');
            setCardMsg('💣 Bomb detonated!');
          }
          if (Array.isArray(ev.payload?.blackHoleExplodedSquares) && ev.payload.blackHoleExplodedSquares.length > 0) {
            const bhSqs = ev.payload.blackHoleExplodedSquares as Sq[];
            triggerSacrificeAnim(bhSqs);
            playSound('capture');
            setCardMsg('🕳️ Black Hole consumed the area!');
          }
        } else if (ev.type === 'card_played') {
          const cardPayload = ev.payload?.card as GameCard | undefined;
          const mechanic = (ev.payload?.mechanic || cardPayload?.mechanic || '') as CardMechanic;
          const cardName = cardPayload?.name || ev.payload?.name || mechanic;
          const isOpponent = !myActor?.playerId || (ev.actorId ? ev.actorId !== myActor.playerId : true);
          if (mechanic === 'reverse') {
            triggerReverseAnim();
            playSound('move');
          }
          if (isOpponent) {
            playCardSound();
            if (mechanic) {
              fireCardAnim(mechanic as any, `Opponent: ${cardName}`);
            }
            setCardMsg(`⚠️ Opponent played ${cardName}!`);
          }
        } else if (ev.type === 'target_selected') {
          const mechanic = ev.payload?.mechanic as string;
          const target = ev.payload?.target as { row: number; col: number } | undefined;
          const piece = ev.payload?.piece as { type: PieceType; color: PieceColor } | undefined;
          const fromSq = ev.payload?.from as { row: number; col: number } | undefined;
          const isOpponent = myActor?.playerId && ev.actorId ? ev.actorId !== myActor.playerId : true;
          if (isOpponent && target) {
            if ((mechanic === 'sniper' || mechanic === 'badsniper') && piece) {
              triggerSniperAnim(target, piece.type, piece.color, mechanic as any);
              fireCardAnim('sniper', `${piece.type} eliminated`);
              playSound('capture');
            } else if (mechanic === 'teleport' && fromSq) {
              triggerTeleportAnim(fromSq, target, piece?.type ?? 'queen', piece?.color ?? 'black');
              playSound('move');
            } else if (mechanic === 'jump' && fromSq) {
              triggerJumpAnim(fromSq, target, piece?.type ?? 'knight', piece?.color ?? 'black', Boolean(piece));
              playSound(piece ? 'capture' : 'move');
            } else if ((mechanic === 'swapme' || mechanic === 'swapus' || mechanic === 'swaphim') && fromSq) {
              triggerSwapAnim(fromSq, target);
              playSound('move');
            } else if (mechanic === 'mindcontrol' && piece) {
              triggerMindControlAnim(target, piece.color, piece.type);
              playSound('card_play');
            } else if ((mechanic === 'smallsacrifice' || mechanic === 'bigsacrifice') && fromSq) {
              triggerSacrificeAnim([fromSq, target]);
              playSound('capture');
            } else if ((mechanic === 'halffuse' || mechanic === 'fullfusion') && fromSq) {
              triggerFuseAnim({
                sq1: fromSq,
                sq2: target,
                type1: piece?.type ?? 'rook',
                type2: 'knight',
                color: piece?.color ?? 'black',
              });
              playSound('card_play');
            } else if (mechanic === 'clone' && fromSq) {
              triggerCloneAnim(fromSq, target, piece?.type ?? 'pawn', piece?.color ?? 'black');
              playSound('move');
            } else if (mechanic === 'blackhole') {
              triggerBlackHoleAnim(target);
              playSound('capture');
            }
          }
        } else if (ev.type === 'card_drawn') {
          const owner = ev.payload?.owner as PieceColor | undefined;
          const isOpponent = !myActor?.playerId || (ev.actorId ? ev.actorId !== myActor.playerId : (mySeat ? owner !== mySeat : owner !== 'white'));
          playCardSound();
          if (isOpponent) {
            // The server stubs the opponent's payload (hidden: true, neutral
            // cards) so their rarity never reaches us. The viewer's own
            // card_drawn event already set the banner with the REAL rarity,
            // so do not overwrite it with a fabricated one -- just signal
            // the opponent's face-down draw via message + sound.
            setCardMsg('🃏 Opponent drew a card!');
          } else {
            const cards = ev.payload?.cards as GameCard[] | undefined;
            const rarity = cards?.[0]?.rarity || 'common';
            setCardMsg('🃏 You drew a card!');
            setLastDrawAnim({ color: (owner || 'white') as any, rarity: rarity as any });
          }
          setTimeout(() => setLastDrawAnim(null), 2500);
        } else if ((ev.type as string) === 'card_draw_lost') {
          const owner = ev.payload?.owner as string | undefined;
          const reason = ev.payload?.reason as string | undefined;
          setCardMsg(`⚠️ ${owner || 'Player'} card draw lost (${reason || 'hand full'})`);
          setTimeout(() => setCardMsg(''), 2500);
        }
      }
    }
    
    const isGameOver = match.status === 'finished';
    setOver(isGameOver);
    setWinner(match.winner ?? null);
    if (match.clock) {
      setTimeW(match.clock.whiteMs);
      setTimeB(match.clock.blackMs);
    }

    if (match.whiteHand) setWhiteHand(match.whiteHand as GameCard[]);
    if (match.blackHand) setBlackHand(match.blackHand as GameCard[]);

    // Re-sync the pending-card UI from the authoritative snapshot. Without
    // this, a play_card intent response or a post-reconnect snapshot restores
    // the board/hands but not the armed target selection -- the server keeps
    // the pending card while the client shows nothing, so the card can never
    // be completed (and every other card click bounces off it).
    setCardPending(buildPendingCardFromSnapshot(
      match.pendingCard ?? null,
      (match.whiteHand as GameCard[] | undefined) ?? [],
      (match.blackHand as GameCard[] | undefined) ?? [],
    ));
    setLavaSquares((match.lavaSquares as any) ?? []);
    setFogZones((match.fogZones as any) ?? []);
    // Radar is now delivered server-side (the snapshot's opposing hand is
    // replaced with real cards while radarRevealFor names this viewer), so
    // mirror the flag into the UI each snapshot.
    setRadarActive(Boolean(match.radarRevealFor));
    setFortressZones((match.fortressZones as any) ?? []);
    setBombPieces((match.bombPieces as any) ?? []);
    setGhostPiece((match.invisiblePiece as any) ?? null);
    setDoubleMove((match.doubleMove as any) ?? null);

    if (isGameOver) {
      setClockActive(false);
      setTicking(null);
    } else if (match.whiteConnected && match.blackConnected) {
      setClockActive(true);
      setTicking(match.turn);
    }
  }, [authoritativeMatchIdRef, authoritativeSeatIdsRef, authoritativeSeatSecretsRef, authoritativeClaimTokensRef, authoritativeClaimExpiresAtRef, blackProfileRef, hostedRuntime, setBoard, setTurn, setMoved, setLm, setHmc, setFmn, setOver, setWinner, setTimeW, setTimeB, setWhiteHand, setBlackHand, setCardPending, setRadarActive, setLavaSquares, setLavaExploding, setFogZones, setFortressZones, setBombPieces, setBombExploding, setGhostPiece, setDoubleMove, setViewerSeat, setMatchSeatMeta, setClockActive, setTicking, viewerSeatRef, whiteProfileRef, setMovHist, fireCardAnim, triggerSniperAnim, triggerTeleportAnim, triggerJumpAnim, triggerSwapAnim, triggerMindControlAnim, triggerSacrificeAnim, triggerFuseAnim, triggerReverseAnim, playCardSound, setCardMsg, setLastDrawAnim, authoritativeActorForColor]);

  const submitAuthoritativeIntent = React.useCallback(async (intent: any) => {
    if (!authoritativeMatchIdRef.current) return;
    if (hostedRuntime && (!intent?.playerId || (!intent?.playerSecret && !intent?.playerClaimToken))) {
      setCardMsg('Live match is still synchronizing your player session. Please try again in a moment.');
      return;
    }
    try {
      const snap = await applyIntent(authoritativeMatchIdRef.current, intent);
      applyAuthoritativeSnapshot(snap);
    } catch (err) {
      // Surface the rejection instead of swallowing it: the server's reason
      // (rate limit, empty text, stale seq, not your turn...) used to be
      // invisible, so every failed chat or move looked like the button did
      // nothing at all. applyIntent already reconciles seq state in the
      // background on failure; this only makes the reason visible.
      const msg = err instanceof Error ? err.message : String(err);
      setCardMsg(`⚠️ ${msg.slice(0, 140)}`);
      window.setTimeout(() => setCardMsg(''), 3500);
    }
  }, [authoritativeMatchIdRef, applyAuthoritativeSnapshot, hostedRuntime, setCardMsg]);

  const {
    cancelCard, getSafeTransforms, getFusedMoves, checkFusionRedundancy, activateDoubleMove,
    openJokerPicker, applyJokerTransform, handlePromoPick, canUseCard, handleCardClick,
    applyCard, getCardHighlight, getDoubleMoveHighlight,
  } = useCardInteraction({
    board, setBoard, turn, setTurn, moved, setMoved, lm, setLm, fmn, fmnRef, boardRef, turnRef,
    whiteHand, setWhiteHand, blackHand, setBlackHand, selectedCard, setSelectedCard,
    cardPending, setCardPending, cardMsg, setCardMsg, promoPicker, setPromoPicker,
    cardPromo, setCardPromo, cardUsedBy, setCardUsedBy, jokerPicker, setJokerPicker,
    doubleMove, setDoubleMove, doubleMoveRef, pendingCardUseRef, cardUsedByRef,
    ghostRef, ghostPiece, setGhostPiece, lavaSquares, setLavaSquares, setLavaExploding,
    bombPieces, setBombPieces, setBombExploding, setSwapAnim, fogZones, setFogZones,
    fortressZones, setFortressZones, authoritativeMatchIdRef, authoritativeActorForColor,
    applyAuthoritativeSnapshot, fireCardAnim, playMoveSound, playCardSound, analyse,
    isAttackedWithFusion,
    checkEndGame: () => {},
    finishCardUse, removeCardFromHand, radarActive, setRadarActive, finalPositionRef,
    setOver, setWinner, setMovHist, setPosHist, setSnapshots, triggerSniperAnim,
    triggerTransformAnim, triggerFuseAnim,
    triggerSwapAnim, triggerTeleportAnim, triggerJumpAnim, triggerMindControlAnim, triggerSacrificeAnim,
    triggerCloneAnim, triggerBlackHoleAnim,
    over, hostedRuntime, viewerSeatRef
  });

  const {
    isAttackedWithFusion, checkEndGame, canSubmitAuthoritativeMove, doMove, doPromo,
    filterFusionChecks, getMoves, canControlColor, canActWithColor, canSelectPiece,
    toggleAnalysisArrow, clearAnalysisArrows, clickSq,
  } = useBoardMoveHandler({
    board, setBoard, turn, setTurn, moved, setMoved, lm, setLm, sel, setSel,
    hints, setHints, drag, setDrag, dragPos, setDragPos, promo, setPromo,
    check, setCheck, mate, setMate, stale, setStale, insuf, setInsuf,
    hmc, setHmc, fmn, setFmn, posHist, setPosHist, drawOffer, setDrawOffer,
    over, setOver, winner, setWinner, movHist, setMovHist, snapshots, setSnapshots,
    analysisArrows, setAnalysisArrows, boardRef, turnRef, movedRef, lmRef,
    hmcRef, fmnRef, posHistRef, overRef, premoveRef, setPremove, doubleMove,
    setDoubleMove, doubleMoveRef, cardPending, selectedCard, setSelectedCard, promoPicker, cardPromo,
    jokerPicker, ghostRef, setGhostPiece, hostedRuntime, viewerSeatRef,
    authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot,
    resetCardUsed, setTicking, setClockActive,
    handleLavaLanding, finalPositionRef, blackMovedRef, setCardMsg, handleCardClick,
    isReviewing, getFusedMoves
  });

  React.useEffect(() => {
    if (over) {
      setPremove(null);
      premoveRef.current = null;
      return;
    }
    const myColor = viewerSeatRef.current ?? (hostedRuntime ? 'white' : turn);
    if (turn !== myColor || !premoveRef.current) return;

    const pm = premoveRef.current;

    // In hosted matches, give the authoritative snapshot a moment to settle
    // before firing: the effect can run before turnRef is in sync with the
    // server's board, causing canSubmitAuthoritativeMove to return false and
    // silently drop the premove. A short delay avoids the race without losing
    // responsiveness (premoves still feel instant to the player).
    const fire = () => {
      // Re-read: the turn or premove may have changed while we were waiting.
      if (overRef.current) return;
      const pm2 = premoveRef.current;
      if (!pm2) return;
      const currentMyColor = viewerSeatRef.current ?? (hostedRuntime ? 'white' : turnRef.current);
      if (turnRef.current !== currentMyColor) return;

      // Only clear premove once we are confident the move will be submitted.
      // canSubmitAuthoritativeMove checks credentials — if not ready yet, keep
      // the premove queued so it fires on the next snapshot instead of being lost.
      if (hostedRuntime && !canSubmitAuthoritativeMove(pm2.from.row, pm2.from.col, pm2.to.row, pm2.to.col)) {
        return; // leave premoveRef intact; will retry when state updates
      }

      const legalMoves = getMoves(pm2.from.row, pm2.from.col);
      const isLegal = legalMoves.some(m => m.row === pm2.to.row && m.col === pm2.to.col);
      setPremove(null);
      premoveRef.current = null;
      if (!isLegal) {
        setCardMsg('⚠️ Premove cancelled: no longer legal');
        setTimeout(() => setCardMsg(''), 2000);
        return;
      }
      doMove(pm2.from.row, pm2.from.col, pm2.to.row, pm2.to.col);
    };

    if (hostedRuntime) {
      const t = setTimeout(fire, 20);
      return () => clearTimeout(t);
    }
    fire();
  }, [turn, over, hostedRuntime, doMove, canSubmitAuthoritativeMove, setPremove, premoveRef, overRef, turnRef, viewerSeatRef, getMoves, setCardMsg]);

  const bootstrapAuthoritativeMatch = React.useCallback(async (options?: { force?: boolean }) => {
    if (!hostedRuntime) return;
    // requestedMatchIdRef is populated at app mount only. After an SPA
    // requestedMatchIdRef is populated at app mount only. After an SPA
    // navigation (queue auto-open, computer match, invite) the URL is the
    // only source of truth -- without this fallback the authoritative
    // bootstrap silently no-ops, authoritativeStatus stays null, and
    // MatchBoardView's `authoritativeStatus !== 'active'` gate swallows
    // every board interaction for the whole match.
    const routedMatchId = typeof window !== 'undefined'
      ? window.location.pathname.match(/^\/match\/([^/?]+)/)?.[1]?.replace(/\/$/, '')
      : null;
    const matchId = requestedMatchIdRef.current
      || gatewayRecoveredMatchIdRef.current
      || (routedMatchId ? decodeURIComponent(routedMatchId) : null)
      || null;
    if (!matchId) return;
    // Explicit retries (the "Retry" buttons) bypass the gone-latch; passive
    // effect runs do not, so a known-gone room cannot spam the error chain
    // on every navigation.
    if (!options?.force && goneMatchIdsRef.current.has(matchId)) return;
    setMatchLoadError(null);
    try {
      const roomMeta = readStoredRoomMeta(matchId);
      const heldCredential = roomMeta?.viewerSeat === 'white'
        ? roomMeta.whitePlayerSecret ?? roomMeta.whiteClaimToken
        : roomMeta?.viewerSeat === 'black'
          ? roomMeta.blackPlayerSecret ?? roomMeta.blackClaimToken
          : null;
      const guest = readStoredGuestIdentity('white');

      // No held credential: either a direct-match bearer URL (join an open
      // seat through the gateway) or a queue-paired player whose seat claim
      // never landed -- the queue auto-open navigates to the room even when
      // its claim POST fails, and the gateway join below CANNOT recover that
      // case because match-service rejects a seat-owner re-join whose seat
      // already carries the server-generated secret. So try the match-claim
      // pipeline first: it authenticates by guest session, recognizes a seat
      // owner from the match archive, and mints/refreshes the real claim;
      // only when that 403/404s (not a participant) fall back to joining as
      // a fresh invitee. A full room falls through to the authenticated
      // fetch below.
      if (!heldCredential && guest.guestId) {
        try {
          const healed = await claimMatchSeat({
            matchId,
            guestId: guest.guestId,
            sessionSecret: guest.sessionSecret,
            sessionToken: guest.sessionToken,
          });
          const seatKey = healed.seatColor === 'black' ? 'black' : 'white';
          const prevSeat = (roomMeta?.viewerSeat ?? null) === healed.seatColor ? roomMeta : null;
          writeStoredRoomMeta(matchId, {
            ...roomMeta,
            viewerSeat: healed.seatColor,
            [`${seatKey}GuestId`]: healed.guestId,
            [`${seatKey}PlayerSecret`]: healed.playerSecret,
            [`${seatKey}ClaimToken`]: healed.claimToken,
            [`${seatKey}ClaimExpiresAt`]: healed.expiresAt ?? '',
            whiteGuestId: healed.whiteGuestId ?? roomMeta?.whiteGuestId,
            blackGuestId: healed.blackGuestId ?? roomMeta?.blackGuestId,
            whiteName: healed.whiteName ?? roomMeta?.whiteName,
            blackName: healed.blackName ?? roomMeta?.blackName,
            queue: healed.queue ?? roomMeta?.queue,
            modeId: healed.modeId ?? roomMeta?.modeId ?? DEFAULT_MATCH_MODE_ID,
            ...(prevSeat ?? {}),
          });
          applyAuthoritativeSnapshot(await fetchMatch(matchId));
          return;
        } catch (claimErr) {
          const status = (claimErr as { status?: number } | null)?.status;
          // 403 = not a seat owner in this match (a fresh invitee); 404 = no
          // claim exists for this guest+match. Both are "try the join path"
          // signals, not failures. Anything else (401 bad session, 5xx,
          // network) is a real error: rethrow so the UI shows a load error
          // instead of silently spectating.
          if (status !== 403 && status !== 404) {
            throw claimErr;
          }
        }
        const account = readStoredAccountIdentity('white');
        try {
          const joined = await joinPrivateMatch({
            matchId,
            identity: {
              guestId: guest.guestId,
              sessionSecret: guest.sessionSecret,
              sessionToken: guest.sessionToken,
              accountId: account.accountId,
              accountSessionToken: account.sessionToken,
            },
          });
          setRatedInviteSignInRequired(false);
          const seatCredentials = joined.seatColor === 'white'
            ? {
              whitePlayerSecret: joined.claim?.playerSecret,
              whiteClaimToken: joined.claim?.claimToken,
              whiteClaimExpiresAt: joined.claim?.expiresAt,
            }
            : {
              blackPlayerSecret: joined.claim?.playerSecret,
              blackClaimToken: joined.claim?.claimToken,
              blackClaimExpiresAt: joined.claim?.expiresAt,
            };
          writeStoredRoomMeta(matchId, {
            ...roomMeta,
            queue: joined.snapshot.match.queue ?? roomMeta?.queue,
            modeId: joined.snapshot.match.modeId ?? roomMeta?.modeId ?? DEFAULT_MATCH_MODE_ID,
            viewerSeat: joined.seatColor,
            whiteGuestId: joined.snapshot.match.whiteGuestId,
            blackGuestId: joined.snapshot.match.blackGuestId,
            whiteAccountId: joined.snapshot.match.whiteAccountId,
            blackAccountId: joined.snapshot.match.blackAccountId,
            whiteName: joined.snapshot.match.whiteName,
            blackName: joined.snapshot.match.blackName,
            ...seatCredentials,
          });
          applyAuthoritativeSnapshot(joined.snapshot);
          return;
        } catch (joinErr) {
          // The gateway rejects a rated private join when the visitor holds no
          // account session ("requires a signed-in account on both sides").
          // Surface the dedicated sign-in prompt for that exact case instead of
          // a dead-end "no access" error; any other failure keeps normal error
          // handling below.
          const joinStatus = (joinErr as { status?: number } | null)?.status;
          const joinMessage = joinErr instanceof Error ? joinErr.message : '';
          if (joinStatus === 403 && /rated|signed-in account/i.test(joinMessage)) {
            setRatedInviteSignInRequired(true);
            setMatchLoadError(null);
            return;
          }
          // A room may already be full. Its seated owner can still fetch a
          // seat-scoped view below; a third party receives the route's normal
          // private-match response instead of any fallback snapshot.
        }
      }
      applyAuthoritativeSnapshot(await fetchMatch(matchId));
    } catch (err) {
      // A failed match hydration must never leave the player on a silently
      // blank/loading board. Surface a real, actionable message; the shell
      // renders it in place of the loading bar on /match/<id>.
      const status = (err as { status?: number } | null)?.status;
      let message = 'Could not load this match. Check your connection and try again.';
      if (status === 404 || status === 410) {
        message = 'This match no longer exists or has finished.';
      } else if (status === 401 || status === 403) {
        message = 'You do not have access to this match room.';
      } else if (status === 429) {
        message = 'Too many requests — wait a moment and try again.';
      }
      if (status === 404 || status === 410) {
        // A 404/410 is the server's terminal verdict for this room, not an
        // application failure: log it as a warning (a console.error on every
        // visit to a stale room read as an app outage) and latch it so later
        // navigations skip the whole bootstrap chain. Seat claims only exist
        // while a match is active, so a finished private match 404s for its
        // own players through the proxy path -- there is no retry that can
        // succeed, and the next computer/queue match overwrites the room
        // meta anyway.
        markMatchGone(matchId);
        goneMatchIdsRef.current.add(matchId);
        console.warn(`[bootstrapAuthoritativeMatch] match unavailable (${status}): ${matchId}`);
      } else {
        console.error('[bootstrapAuthoritativeMatch] failed:', err);
      }
      setMatchLoadError(message);
    }
  }, [hostedRuntime, requestedMatchIdRef, gatewayRecoveredMatchIdRef, applyAuthoritativeSnapshot]);

  // A route change only updates refs and router state; it does not itself
  // hydrate the match. Watching the routed match surface makes every direct
  // /match/<id> navigation hydrate through the same gateway-first path as a
  // fresh page load, including newly created computer matches. A fresh invitee receives
  // its guest identity asynchronously from the initial gateway bootstrap;
  // wait for that before attempting to claim the open seat. Otherwise the
  // first fetch succeeds as an anonymous spectator and no later dependency
  // change retries the join.
  React.useEffect(() => {
    if (!hostedRuntime || !guestProfilesReady || (!pathname.startsWith('/match/') && activePage !== 'Match')) return;
    void bootstrapAuthoritativeMatch();
  }, [activePage, bootstrapAuthoritativeMatch, guestProfilesReady, hostedRuntime, pathname]);

  // Leaving a gone room drops its stale seat/claim state: the match is
  // unreadable, so the cached credentials can only produce failed heartbeats
  // and claim refreshes later. Kept out of bootstrapAuthoritativeMatch because
  // it already re-runs on every navigation; this runs once per page transition.
  React.useEffect(() => {
    if (pathname?.startsWith('/match/')) return;
    const routedMatchId = typeof window !== 'undefined'
      ? window.location.pathname.match(/^\/match\/([^/?]+)/)?.[1]?.replace(/\/$/, '')
      : null;
    const matchId = requestedMatchIdRef.current
      || gatewayRecoveredMatchIdRef.current
      || (routedMatchId ? decodeURIComponent(routedMatchId) : null)
      || null;
    if (!matchId || !goneMatchIdsRef.current.has(matchId)) return;
    goneMatchIdsRef.current.delete(matchId);
    writeStoredActiveMatchId(null);
    writeStoredRoomMeta(matchId, null);
    requestedMatchIdRef.current = null;
    gatewayRecoveredMatchIdRef.current = null;
  }, [pathname, requestedMatchIdRef, gatewayRecoveredMatchIdRef]);

  const resetBoardEffectsCallback = React.useCallback(() => {
    setLavaSquares([]);
    setLavaExploding([]);
    setFogZones([]);
    setFortressZones([]);
    setGhostPiece(null);
    if (ghostRef) ghostRef.current = null;
  }, [setLavaSquares, setLavaExploding, setFogZones, setFortressZones, setGhostPiece, ghostRef]);

  const newGame = React.useCallback(() => {
    stop();
    // Captured before the refs below are nulled: the finished room's meta is
    // cleared with the game so its seat credentials cannot leak into a new
    // room's auth path.
    const finishedMatchId = authoritativeMatchIdRef.current;
    setBoard(makeBoard());
    setTurn('white');
    setSel(null);
    setHints([]);
    setMoved(new Set());
    setLm(null);
    setDrag(null);
    setPromo(null);
    setCheck(false);
    setMate(false);
    setStale(false);
    setInsuf(false);
    setHmc(0);
    setFmn(1);
    setPosHist([]);
    setDrawOffer(null);
    setOver(false);
    setWinner(null);
    setMovHist([]);
    setSnapshots([]);
    setReviewIdx(-1);
    setReviewBoard(null);
    setEngineOn(false);
    resetChat();
    resetTimer();
    blackMovedRef.current = false;
    finalPositionRef.current = null;
    cardUsedByRef.current = { white: false, black: false };
    setCardUsedBy({ white: false, black: false });
    pendingCardUseRef.current = new Set();
    setSelectedCard(null);
    setWhiteHand([]);
    setBlackHand([]);
    setLastDrawAnim(null);
    setDealPhase('idle');
    setCardPending(null);
    setCardMsg('');
    setPromoPicker(null);
    resetBoardEffectsCallback();
    setBombPieces([]);
    setBombExploding([]);
    setSwapAnim(null);
    setJokerPicker(null);
    resetAntiCheat();
    setCardPromo(null);
    setDoubleMove(null);
    setCardAnim(null);
    setViewerSeat(null);
    viewerSeatRef.current = null;
    setMatchSeatMeta(null);
    setAuthoritativeLive(false);
    setAuthoritativeMatchId(null);
    setAuthoritativeStatus(null);
    setAuthoritativeFinishReason(null);
    setAuthoritativeWhiteConnected(false);
    setAuthoritativeBlackConnected(false);
    setAuthoritativeDisconnectGraceFor(null);
    setAuthoritativeDisconnectGraceDeadline(null);
    authoritativeMatchIdRef.current = null;
    authoritativeSeatSecretsRef.current = { white: null, black: null };
    authoritativeClaimExpiresAtRef.current = { white: null, black: null };
    authoritativeClaimTokensRef.current = { white: null, black: null };
    gatewayBootstrapClaimsRef.current = { matchId: null, whiteSecret: null, blackSecret: null, whiteToken: null, blackToken: null, whiteExpiresAt: null, blackExpiresAt: null };
    gatewayRecoveredMatchIdRef.current = null;
    requestedMatchIdRef.current = null;
    writeStoredActiveMatchId(null);
    if (finishedMatchId) writeStoredRoomMeta(finishedMatchId, null);
    clearRequestedMatchQuery();
    setGameKey(k => k + 1);
    if (hostedRuntime) {
      setActivePage('Play');
      return;
    }
    void bootstrapAuthoritativeMatch({ force: true });
  }, [stop, setBoard, setTurn, setSel, setHints, setMoved, setLm, setDrag, setPromo, setCheck, setMate, setStale, setInsuf, setHmc, setFmn, setPosHist, setDrawOffer, setOver, setWinner, setMovHist, setSnapshots, setReviewIdx, setReviewBoard, resetChat, resetTimer, cardUsedByRef, setCardUsedBy, pendingCardUseRef, setSelectedCard, setWhiteHand, setBlackHand, setLastDrawAnim, setDealPhase, setCardPending, setCardMsg, setPromoPicker, resetBoardEffectsCallback, setBombPieces, setBombExploding, setSwapAnim, setJokerPicker, resetAntiCheat, setCardPromo, setDoubleMove, setCardAnim, setViewerSeat, viewerSeatRef, setMatchSeatMeta, authoritativeMatchIdRef, authoritativeSeatSecretsRef, authoritativeClaimExpiresAtRef, authoritativeClaimTokensRef, gatewayBootstrapClaimsRef, gatewayRecoveredMatchIdRef, requestedMatchIdRef, hostedRuntime, setActivePage, bootstrapAuthoritativeMatch]);

  const returnToQueueHome = React.useCallback(() => {
    setQueueLaunchIntent(null);
    newGame();
  }, [newGame, setQueueLaunchIntent]);

  const returnToSameQueueLane = React.useCallback(() => {
    if (!authoritativeMatchId) {
      returnToQueueHome();
      return;
    }
    const roomMeta = readStoredRoomMeta(authoritativeMatchId);
    if (roomMeta?.queue === 'casual' || roomMeta?.queue === 'rated') {
      setQueueLaunchIntent({
        queue: roomMeta.queue,
        modeId: roomMeta.modeId ?? DEFAULT_MATCH_MODE_ID,
      });
      newGame();
      return;
    }
    returnToQueueHome();
  }, [authoritativeMatchId, newGame, returnToQueueHome, setQueueLaunchIntent]);

  const nav = useMatchNav({
    activePage, setActivePage, hostedRuntime, pathname,
    viewerSeat, whiteProfile, blackProfile,
    primaryAccountIdentity,
    inboxUnreadCount, friendsAttentionCount,
    authoritativeLive, authoritativeStatus,
    authoritativeMatchId, authoritativeRematchBusy,
    socialAlert, setSocialAlert,
    authoritativeDisconnectGraceDeadline,
    authoritativeDisconnectGraceFor,
    authoritativeWhiteConnected, authoritativeBlackConnected,
    authoritativeFinishReason, matchSeatMeta, timeW, timeB,
    clockActive, tickingState, over,
    whiteHand, blackHand,
    winner, hmc, stale, insuf, mate,
    turn,
    openLiveMatch,
    dismissedSocialAlertIdsRef,
    authoritativeActionReady: !hostedRuntime || (!!viewerSeat && (() => {
      const actor = authoritativeActorForColor(viewerSeat);
      return !!actor.playerId && !!(actor.playerSecret || actor.playerClaimToken);
    })()),
  });

  const { onStreamReconnect } = useMatchConnection({
    sets: {
      setAuthoritativeMatchId,
      setAuthoritativeLive,
      setStreamDisconnected,
      setAuthoritativeStatus,
      setAuthoritativeWhiteConnected,
      setAuthoritativeBlackConnected,
      setAuthoritativeDisconnectGraceFor,
      setAuthoritativeDisconnectGraceDeadline,
      setViewerSeat,
      setMatchSeatMeta,
      setCardMsg,
      setAuthoritativeRematchBusy,
      setMatchDestinationNotice,
      setActivePage,
    },
    authoritativeMatchId,
    authoritativeMatchIdRef,
    authoritativeClaimTokensRef,
    authoritativeClaimExpiresAtRef,
    hostedRuntime,
    viewerSeat,
    over,
    primaryAccountIdentity,
    openLiveMatch,
    buildGatewayBootstrapRequest,
    applyGatewayGuestSessions,
    applyGatewayMatchClaims,
    applyGatewayAccountSessions,
    onSnapshot: applyAuthoritativeSnapshot,
    authoritativeActorForColor,
  });

  const {
    displayedWhiteName, displayedBlackName, displayedWhiteRating, displayedBlackRating,
    whiteSeatBadge, blackSeatBadge,
  } = nav;

  const {
    fmtClock, evalStr, evalLabel, renderPlayerCard, renderJokerPicker,
  } = useMatchUIHelpers({
    displayedWhiteName, displayedBlackName, displayedWhiteRating, displayedBlackRating,
    whiteSeatBadge, blackSeatBadge, timeW, timeB, tickingState, clockActive, over,
    jokerPicker, setJokerPicker, cancelCard, applyJokerTransform,
    authoritativeMatchIdRef, jokerRef,
  });

  const kingPos = check && !isReviewing ? findKing(board, turn) : null;
  const roundNumber = React.useMemo(() => Math.floor(fmn), [fmn]);
  const hasPrimaryAccountSession = !!primaryAccountIdentity?.sessionToken;

  const createAuthoritativeRematchRoom = React.useCallback(async () => {
    const matchId = authoritativeMatchIdRef.current;
    if (!matchId) return;
    const roomMeta = readStoredRoomMeta(matchId);
    if (roomMeta?.queue !== 'direct') return;
    const guestIdentity = readStoredGuestIdentity('white');
    if (!guestIdentity.guestId) {
      setMatchDestinationNotice('Hosted player session is still loading, so rematch room creation is not ready yet.');
      return;
    }
    setAuthoritativeRematchBusy(true);
    setMatchDestinationNotice('');
    try {
      const result = await rematchPrivateMatch({
        matchId,
        identity: {
          guestId: guestIdentity.guestId,
          sessionSecret: guestIdentity.sessionSecret,
          sessionToken: guestIdentity.sessionToken,
          accountId: primaryAccountIdentity?.accountId,
          accountSessionToken: primaryAccountIdentity?.sessionToken,
        },
        clockSeconds: roomMeta?.clockSeconds ?? 600,
        difficulty: roomMeta?.difficulty ?? '',
      });
      writeStoredRoomMeta(result.matchId, {
        queue: 'direct',
        modeId: result.snapshot.match.modeId ?? roomMeta?.modeId,
        clockSeconds: roomMeta?.clockSeconds ?? 600,
        viewerSeat: result.seatColor,
        whiteGuestId: result.snapshot.match.whiteGuestId,
        blackGuestId: result.snapshot.match.blackGuestId,
        whiteAccountId: result.snapshot.match.whiteAccountId,
        blackAccountId: result.snapshot.match.blackAccountId,
        whiteName: result.snapshot.match.whiteName,
        blackName: result.snapshot.match.blackName,
        whitePlayerSecret: result.seatColor === 'white' ? result.claim?.playerSecret : undefined,
        blackPlayerSecret: result.seatColor === 'black' ? result.claim?.playerSecret : undefined,
        whiteClaimToken: result.seatColor === 'white' ? result.claim?.claimToken : undefined,
        blackClaimToken: result.seatColor === 'black' ? result.claim?.claimToken : undefined,
        whiteClaimExpiresAt: result.seatColor === 'white' ? result.claim?.expiresAt : undefined,
        blackClaimExpiresAt: result.seatColor === 'black' ? result.claim?.expiresAt : undefined,
      });
      writeStoredActiveMatchId(result.matchId);
      setMatchDestinationNotice('Rematch room created. Opening it now...');
      openLiveMatch(result.matchId);
    } catch (err) {
      setMatchDestinationNotice(err instanceof Error ? err.message : 'Failed to create private rematch room.');
    } finally {
      setAuthoritativeRematchBusy(false);
    }
  }, [authoritativeMatchIdRef, openLiveMatch, primaryAccountIdentity?.accountId, primaryAccountIdentity?.sessionToken, setAuthoritativeRematchBusy, setMatchDestinationNotice]);

  return {
    sfReady, isThinking, ev, sfErr, analyse, stop, resetEval,
    ...nav,
    createAuthoritativeRematchRoom,
    authoritativeRematchBusy, setAuthoritativeRematchBusy,
    hostedRuntime, viewerSeat, viewerSeatRef, authoritativeMatchIdRef, onStreamReconnect,
    authoritativeMatchId, setAuthoritativeMatchId, primaryAccountIdentity,
    board, setBoard, turn, setTurn, sel, setSel, hints, setHints, moved, setMoved,
    lm, setLm, drag, setDrag, dragPos, setDragPos, promo, setPromo,
    check, setCheck, mate, setMate, stale, setStale, insuf, setInsuf,
    hmc, setHmc, fmn, setFmn, posHist, setPosHist, drawOffer, setDrawOffer,
    over, setOver, winner, setWinner, authoritativeFinishReason, setAuthoritativeFinishReason,
    movHist, setMovHist, snapshots, setSnapshots, reviewIdx, setReviewIdx,
    analysisArrows, setAnalysisArrows,
    openProfileHandle, openReplayMatch, openGuestHistory, openLiveMatch,
    copyLiveMatchLink, copyReplayPageLink, dismissedSocialAlertIdsRef,
    setPrimaryAccountIdentity, shellAccountNotice, setShellAccountNotice,
    syncPrimaryAccountIdentity, clearPrimaryAccountRestriction, pulseSocialLive,
    handleSeatAuthenticated, handlePrimaryShellAuthenticated,
    whiteHand, setWhiteHand, blackHand, setBlackHand,
    selectedCard, setSelectedCard, dealPhase, setDealPhase,
    lastDrawAnim, setLastDrawAnim, cardPending, setCardPending,
    cardMsg, setCardMsg, promoPicker, setPromoPicker,
    cardPromo, setCardPromo, cardUsedBy, setCardUsedBy,
    jokerPicker, setJokerPicker, cardAnim, setCardAnim,
    cardAnimLbl, setCardAnimLbl, fireCardAnim,
    bombPieces, setBombPieces, bombExploding, setBombExploding,
    swapAnim, setSwapAnim, doubleMove, setDoubleMove,
    ghostPiece, ghostRef, radarActive, setRadarActive,
    lavaSquares, lavaExploding, fogZones, fortressZones,
    triggerSniperAnim, triggerTransformAnim, triggerFuseAnim,
    transformAnim, sniperAnim, teleportAnim, jumpAnim, sacrificeAnim, mindControlAnim, fuseAnim,
    reverseAnim, triggerReverseAnim,
    cloneAnim, triggerCloneAnim, blackHoleAnim, triggerBlackHoleAnim, poofAnim, triggerPoofAnim,
    timeW, setTimeW, timeB, setTimeB, tickingState, setTicking,
    clockActive, setClockActive, resetTimer,
    authoritativeLive, authoritativeStatus,
    authoritativeWhiteConnected, authoritativeBlackConnected,
    authoritativeDisconnectGraceFor, authoritativeDisconnectGraceDeadline,
    authoritativeActorForColor,
    intentInFlight,
    isAttackedWithFusion, checkEndGame, handleLavaLanding,
    canSubmitAuthoritativeMove, doMove, doPromo,
    removeCardFromHand, finishCardUse, jokerRef,
    cancelCard, getSafeTransforms, getFusedMoves,
    checkFusionRedundancy, activateDoubleMove,
    openJokerPicker, applyJokerTransform,
    handleCardClick, handlePromoPick, canUseCard, applyCard,
    newGame, returnToQueueHome, returnToSameQueueLane,
    goToSnap, reviewFirst, reviewPrev, reviewNext, reviewLast,
    isReviewing, kingPos, filterFusionChecks, getMoves,
    canControlColor, canActWithColor, canSelectPiece,
    toggleAnalysisArrow, clearAnalysisArrows, clickSq,
    getDoubleMoveHighlight, getCardHighlight,
    fmtClock, evalStr, evalLabel, renderPlayerCard, renderJokerPicker,
    premove, setPremove, premoveRef,
    chatMessages, setChatMessages, chatInput, setChatInput, chatRef, resetChat,
    roundNumber, streamDisconnected, hasPrimaryAccountSession,
    submitAuthoritativeIntent, bootstrapAuthoritativeMatch, requestedMatchIdRef,
    matchLoadError, setMatchLoadError,
    ratedInviteSignInRequired, setRatedInviteSignInRequired,
    engineOn, setEngineOn, finalPositionRef, reviewBoard,
  };
}
