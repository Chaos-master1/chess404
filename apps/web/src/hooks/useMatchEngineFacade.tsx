'use client';

import React from 'react';
import { useRouter } from 'next/navigation';
import type {
  MatchModeId,
  MatchSnapshotMessage,
  PlayerIntent,
} from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
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
} from '../types';
import {
  makeBoard,
  findKing,
} from '../chessEngine';
import {
  OPP,
} from '../constants';
import {
  applyIntent,
  fetchMatch,
  readStoredRoomMeta,
  resolveSeatSecret,
  type StoredRoomMeta,
  writeStoredRoomMeta,
} from '../lib/match-service';
import { rematchPrivateMatch } from '../lib/private-match-service';
import {
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
  | 'Modes'
  | 'Queue'
  | 'Lobbies';

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
    abortCountdown, setAbortCountdown, startAbortCountdown, stopAbortCountdown, resetTimer,
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
  const playCardSound = React.useCallback(() => playSound('card_play'), []);
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
  const [authoritativeStatus, setAuthoritativeStatus] = React.useState<'waiting' | 'active' | 'finished' | null>(null);
  const [authoritativeWhiteConnected, setAuthoritativeWhiteConnected] = React.useState(false);
  const [authoritativeBlackConnected, setAuthoritativeBlackConnected] = React.useState(false);
  const [authoritativeDisconnectGraceFor, setAuthoritativeDisconnectGraceFor] = React.useState<PieceColor | null>(null);
  const [authoritativeDisconnectGraceDeadline, setAuthoritativeDisconnectGraceDeadline] = React.useState<string | null>(null);

  const authoritativeActorForColor = React.useCallback((color: PieceColor): { playerId: string; playerSecret?: string; playerClaimToken?: string } => {
    const seatId = authoritativeSeatIdsRef.current[color];
    const seatSecret = authoritativeSeatSecretsRef.current[color];
    const claimToken = authoritativeClaimTokensRef.current[color];
    const claimExpiresAt = authoritativeClaimExpiresAtRef.current[color];
    const tokenValid = !!claimToken && (!claimExpiresAt || new Date(claimExpiresAt).getTime() > Date.now());
    return {
      playerId: seatId || (color === 'white' ? 'white' : 'black'),
      playerSecret: seatSecret || undefined,
      playerClaimToken: tokenValid ? claimToken : undefined,
    };
  }, [authoritativeClaimExpiresAtRef, authoritativeClaimTokensRef, authoritativeSeatIdsRef, authoritativeSeatSecretsRef]);

  const applyAuthoritativeSnapshot = React.useCallback((snapshot: MatchSnapshotMessage) => {
    const match = snapshot.match;
    if (!match) return;
    setAuthoritativeMatchId(match.matchId);
    authoritativeMatchIdRef.current = match.matchId;
    setAuthoritativeLive(match.status === 'active' || match.status === 'waiting');
    setAuthoritativeStatus(match.status);
    setAuthoritativeFinishReason(match.finishReason ?? null);
    setAuthoritativeWhiteConnected(match.whiteConnected);
    setAuthoritativeBlackConnected(match.blackConnected);
    setAuthoritativeDisconnectGraceFor(match.disconnectGraceFor ?? null);
    setAuthoritativeDisconnectGraceDeadline(match.disconnectGraceDeadline ?? null);

    setBoard(match.board as Board);
    setTurn(match.turn);
    setMoved(new Set(match.moved));
    setLm(match.lastMove);
    setHmc(match.halfMoveClock);
    setFmn(match.fullMoveNumber);
    
    const isGameOver = match.status === 'finished';
    setOver(isGameOver);
    setWinner(match.winner ?? null);
    if (match.clock) {
      setTimeW(match.clock.whiteMs);
      setTimeB(match.clock.blackMs);
    }

    if (match.whiteHand) setWhiteHand(match.whiteHand as GameCard[]);
    if (match.blackHand) setBlackHand(match.blackHand as GameCard[]);
    if (match.lavaSquares) setLavaSquares(match.lavaSquares as any);
    if (match.fogZones) setFogZones(match.fogZones as any);
    if (match.fortressZones) setFortressZones(match.fortressZones as any);
    if (match.bombPieces) setBombPieces(match.bombPieces as any);

    if (isGameOver) {
      stopAbortCountdown();
      setClockActive(false);
      setTicking(null);
    } else if (match.whiteConnected && match.blackConnected) {
      setClockActive(true);
      setTicking(match.turn);
    }
  }, [authoritativeMatchIdRef, setBoard, setTurn, setMoved, setLm, setHmc, setFmn, setOver, setWinner, setTimeW, setTimeB, setWhiteHand, setBlackHand, setLavaSquares, setFogZones, setFortressZones, setBombPieces, stopAbortCountdown, setClockActive, setTicking]);

  const submitAuthoritativeIntent = React.useCallback(async (intent: any) => {
    if (!authoritativeMatchIdRef.current) return;
    try {
      const snap = await applyIntent(authoritativeMatchIdRef.current, intent);
      applyAuthoritativeSnapshot(snap);
    } catch {
      // Reconcile on failure
    }
  }, [authoritativeMatchIdRef, applyAuthoritativeSnapshot]);

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
    isAttackedWithFusion: (b, r, c, by) => findKing(b, by) !== null,
    checkEndGame: () => {},
    finishCardUse, removeCardFromHand, radarActive, setRadarActive, finalPositionRef,
    setOver, setWinner, setMovHist, setPosHist, setSnapshots, triggerSniperAnim,
    triggerTransformAnim, triggerFuseAnim, over, hostedRuntime, viewerSeatRef
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
    setDoubleMove, doubleMoveRef, cardPending, selectedCard, promoPicker, cardPromo,
    jokerPicker, ghostRef, setGhostPiece, hostedRuntime, viewerSeatRef,
    authoritativeMatchIdRef, authoritativeActorForColor, applyAuthoritativeSnapshot,
    resetCardUsed, startAbortCountdown, stopAbortCountdown, setTicking, setClockActive,
    handleLavaLanding, finalPositionRef, blackMovedRef, setCardMsg, handleCardClick,
    isReviewing, getFusedMoves
  });

  const bootstrapAuthoritativeMatch = React.useCallback(async () => {
    if (!hostedRuntime) return;
    const matchId = requestedMatchIdRef.current || gatewayRecoveredMatchIdRef.current;
    if (!matchId) return;
    try {
      const snapshot = await fetchMatch(matchId);
      applyAuthoritativeSnapshot(snapshot);
    } catch {
      // Keep offline or initial state
    }
  }, [hostedRuntime, requestedMatchIdRef, gatewayRecoveredMatchIdRef, applyAuthoritativeSnapshot]);

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
    clearRequestedMatchQuery();
    setGameKey(k => k + 1);
    if (hostedRuntime) {
      setActivePage('Play');
      return;
    }
    setTimeout(() => startAbortCountdown(), 0);
    void bootstrapAuthoritativeMatch();
  }, [stop, setBoard, setTurn, setSel, setHints, setMoved, setLm, setDrag, setPromo, setCheck, setMate, setStale, setInsuf, setHmc, setFmn, setPosHist, setDrawOffer, setOver, setWinner, setMovHist, setSnapshots, setReviewIdx, setReviewBoard, resetChat, resetTimer, cardUsedByRef, setCardUsedBy, pendingCardUseRef, setSelectedCard, setWhiteHand, setBlackHand, setLastDrawAnim, setDealPhase, setCardPending, setCardMsg, setPromoPicker, resetBoardEffectsCallback, setBombPieces, setBombExploding, setSwapAnim, setJokerPicker, resetAntiCheat, setCardPromo, setDoubleMove, setCardAnim, setViewerSeat, viewerSeatRef, setMatchSeatMeta, authoritativeMatchIdRef, authoritativeSeatSecretsRef, authoritativeClaimExpiresAtRef, authoritativeClaimTokensRef, gatewayBootstrapClaimsRef, gatewayRecoveredMatchIdRef, requestedMatchIdRef, hostedRuntime, setActivePage, startAbortCountdown, bootstrapAuthoritativeMatch]);

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
  const abortActive = abortCountdown !== null && abortCountdown > 0;
  const streamDisconnected = false;
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

  const onStreamReconnect = React.useCallback(() => {
    void fetchMatch(authoritativeMatchIdRef.current || '').then(applyAuthoritativeSnapshot).catch(() => {});
  }, [authoritativeMatchIdRef, applyAuthoritativeSnapshot]);

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
    timeW, setTimeW, timeB, setTimeB, tickingState, setTicking,
    clockActive, setClockActive, abortCountdown, startAbortCountdown,
    stopAbortCountdown, resetTimer,
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
    roundNumber, abortActive, streamDisconnected, hasPrimaryAccountSession,
    submitAuthoritativeIntent, bootstrapAuthoritativeMatch, requestedMatchIdRef,
    engineOn, setEngineOn, finalPositionRef, reviewBoard,
  };
}