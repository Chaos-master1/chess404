import { redirect } from 'next/navigation';
import Link from 'next/link';

function firstParam(value: string | string[] | undefined): string {
  if (Array.isArray(value)) {
    return value[0]?.trim() ?? '';
  }
  return value?.trim() ?? '';
}

const FEATURES = [
  {
    title: 'Chess + Cards',
    body: 'Every game combines classic chess with tactical card abilities. Freeze enemy pieces, teleport across the board, or shield your king.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="13" height="17" rx="2" />
        <path d="M16 8.5 19.4 7a1.6 1.6 0 0 1 2.1.9l1.2 3.2a1.6 1.6 0 0 1-.9 2.1L19 14.6" />
        <path d="M7 9h5M7 12.5h5M7 16h3" />
      </svg>
    ),
  },
  {
    title: 'Ranked Play',
    body: 'Climb the leaderboard with competitive matchmaking. Time controls, draws, and resignations all supported.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M8 21h8M12 17v4M7 4h10v4a5 5 0 0 1-10 0V4Z" />
        <path d="M7 6H4a1 1 0 0 0-1 1c0 2.2 1.8 4 4 4M17 6h3a1 1 0 0 1 1 1c0 2.2-1.8 4-4 4" />
      </svg>
    ),
  },
  {
    title: 'Guest or Account',
    body: 'Jump in as a guest instantly or create an account to save your progress, stats, and history.',
    icon: (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="9" cy="8" r="3.2" />
        <path d="M3.5 20a5.5 5.5 0 0 1 11 0" />
        <path d="M16 4.6a3.2 3.2 0 0 1 0 6.8M17.5 14.6a5.5 5.5 0 0 1 3 4.9" />
      </svg>
    ),
  },
] as const;

export default async function HomePage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const requestedMatchId = firstParam(params.match);
  const requestedReplayMatchId = firstParam(params.replay);
  const requestedGuestId = firstParam(params.guest);
  const requestedProfileHandle = firstParam(params.profile).toLowerCase();
  const requestedAuthAction = firstParam(params.auth);
  const requestedAuthToken = firstParam(params.token);
  const requestedAccountId = firstParam(params.account);

  if (requestedMatchId) {
    redirect(`/match/${encodeURIComponent(requestedMatchId)}`);
  }

  if (requestedReplayMatchId || requestedGuestId) {
    const historyParams = new URLSearchParams();
    if (requestedReplayMatchId) {
      historyParams.set('replay', requestedReplayMatchId);
    }
    if (requestedGuestId) {
      historyParams.set('guest', requestedGuestId);
    }
    redirect(`/history${historyParams.size ? `?${historyParams.toString()}` : ''}`);
  }

  if (requestedProfileHandle) {
    const profileParams = new URLSearchParams({ profile: requestedProfileHandle });
    redirect(`/profiles?${profileParams.toString()}`);
  }

  if (
    (requestedAuthAction === 'verify-email' || requestedAuthAction === 'reset-password') &&
    requestedAuthToken
  ) {
    const accountParams = new URLSearchParams({
      auth: requestedAuthAction,
      token: requestedAuthToken,
    });
    if (requestedAccountId) {
      accountParams.set('account', requestedAccountId);
    }
    redirect(`/account?${accountParams.toString()}`);
  }

  return (
    <div className="landing-page">
      <section className="landing-hero">
        <span className="hero-eyebrow">
          <span className="hero-eyebrow-dot" />
          Live now — casual &amp; rated queues open
        </span>
        <h1 className="hero-title">Chess404</h1>
        <p className="hero-subtitle">
          Competitive online chess with curated card powers. Outplay, outwit, outshine.
        </p>
        <div className="hero-actions">
          <Link href="/play" className="btn-primary">Play Now</Link>
          <Link href="/watch" className="btn-secondary">Watch Games</Link>
        </div>
        <div className="hero-stats">
          <span>37 cards</span>
          <span className="hero-stats-sep" />
          <span>5 engine levels</span>
          <span className="hero-stats-sep" />
          <span>Free to play</span>
        </div>
      </section>
      <section className="features">
        {FEATURES.map((feature) => (
          <div className="feature-card" key={feature.title}>
            <div className="feature-card__icon">{feature.icon}</div>
            <h3>{feature.title}</h3>
            <p>{feature.body}</p>
          </div>
        ))}
      </section>
    </div>
  );
}
