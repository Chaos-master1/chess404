import type { Metadata } from 'next';
import Link from 'next/link';

export const metadata: Metadata = {
  title: 'Privacy Policy — Chess404',
  description: 'What data Chess404 collects, why, and the choices you have.',
};

const pageStyle: React.CSSProperties = {
  minHeight: '100vh',
  background: 'radial-gradient(1200px 600px at 50% -10%, #131a2b 0%, #0a0d16 60%)',
  color: '#f3e6bf',
  padding: '48px 20px 80px',
};

const articleStyle: React.CSSProperties = {
  maxWidth: '760px',
  margin: '0 auto',
  lineHeight: 1.7,
  fontSize: '15px',
};

const sectionStyle: React.CSSProperties = {
  marginTop: '28px',
};

export default function PrivacyRoute() {
  return (
    <main style={pageStyle}>
      <article style={articleStyle}>
        <p style={{ letterSpacing: '2px', textTransform: 'uppercase', color: '#ffbe5a', fontWeight: 800, fontSize: '12px', margin: 0 }}>
          Chess404
        </p>
        <h1 style={{ fontSize: '32px', margin: '8px 0 4px' }}>Privacy Policy</h1>
        <p style={{ color: 'rgba(243,230,191,0.65)', margin: 0 }}>Last updated: September 21, 2026</p>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>1. What we collect</h2>
          <p>
            <strong>Account data:</strong> if you register, we store the handle, email address, and a salted hash of
            your password. We never store passwords in plain text. <strong>Game data:</strong> moves, cards played, and
            match outcomes are stored to run games, maintain rankings, and let you review your history.
            <strong> Guest sessions:</strong> playing without an account creates an anonymous guest identifier stored
            in your browser; it contains no personal information.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>2. What we do not collect</h2>
          <p>
            Chess404 runs no advertising trackers and sells no data. We do not collect payment information — the
            Service is currently free to play.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>3. How data is used</h2>
          <p>
            Data is used to operate the Service: authenticating you, matching you with opponents, enforcing fair play,
            showing your stats and history, and sending transactional email such as address verification and password
            resets. We never use your email address for marketing without your consent.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>4. Data retention</h2>
          <p>
            Game history is retained to power profiles and rankings. You can delete your account at any time from the
            account page; this removes your email address and handle from active systems. Residual copies may persist
            in encrypted backups for a limited window before being purged.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>5. Your rights</h2>
          <p>
            You can access and update your handle and email from the account page, request a copy of your data, or
            request deletion of your account and personal data through the in-app support channels. We will respond to
            verified requests within a reasonable timeframe.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>6. Security</h2>
          <p>
            Sessions are authenticated with rotating tokens; game state is validated server-authoritatively; secrets
            are never exposed to other players. Transport is encrypted over HTTPS/WSS.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>7. Children</h2>
          <p>
            The Service is not directed at children under 13, and we do not knowingly collect personal information
            from them. If you believe a child under 13 has created an account, contact us and we will remove it.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>8. Changes and contact</h2>
          <p>
            We will post any changes to this policy on this page with an updated date. Questions can be sent through
            the in-app support channels. See also our{' '}
            <Link href="/terms" style={{ color: '#ffbe5a' }}>Terms of Service</Link>.
          </p>
        </section>

        <p style={{ marginTop: '40px' }}>
          <Link href="/" style={{ color: '#ffbe5a' }}>← Back to Chess404</Link>
        </p>
      </article>
    </main>
  );
}
