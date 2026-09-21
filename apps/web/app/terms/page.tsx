import type { Metadata } from 'next';
import Link from 'next/link';

export const metadata: Metadata = {
  title: 'Terms of Service — Chess404',
  description: 'The rules for using Chess404: accounts, fair play, and content ownership.',
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

export default function TermsRoute() {
  return (
    <main style={pageStyle}>
      <article style={articleStyle}>
        <p style={{ letterSpacing: '2px', textTransform: 'uppercase', color: '#ffbe5a', fontWeight: 800, fontSize: '12px', margin: 0 }}>
          Chess404
        </p>
        <h1 style={{ fontSize: '32px', margin: '8px 0 4px' }}>Terms of Service</h1>
        <p style={{ color: 'rgba(243,230,191,0.65)', margin: 0 }}>Last updated: September 21, 2026</p>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>1. Accepting these terms</h2>
          <p>
            By creating an account or playing Chess404 (&quot;the Service&quot;) you agree to these Terms of Service.
            If you do not agree, do not use the Service. You must be at least 13 years old to create an account.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>2. Accounts</h2>
          <p>
            You may play as a guest or register an account with a handle, email address, and password. You are
            responsible for keeping your credentials confidential and for all activity under your account. Notify us
            immediately if you believe your account has been compromised. Accounts are personal and may not be sold,
            shared, or transferred.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>3. Fair play</h2>
          <p>
            The Service is a competitive game. You may not use bots, scripts, engine assistance, multiple simultaneous
            sessions, or any automation to gain an unfair advantage; tamper with the Service, its servers, or other
            players&apos; clients; exploit bugs for rating or rewards instead of reporting them; harass, threaten, or
            abuse other players. We may investigate and suspend or terminate accounts that violate these rules.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>4. Cards and game rules</h2>
          <p>
            Chess404 combines standard chess with card abilities. Card powers and rule interactions are applied by the
            game server and may be rebalanced, renamed, or changed at any time to keep play fair and interesting.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>5. Availability and changes</h2>
          <p>
            We aim to keep the Service available but provide it &quot;as is&quot; without warranties of any kind. We may
            modify, suspend, or discontinue any part of the Service at any time. We may update these terms; continued
            use after an update constitutes acceptance of the revised terms.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>6. Limitation of liability</h2>
          <p>
            To the maximum extent permitted by law, Chess404 and its operators are not liable for indirect,
            incidental, or consequential damages, loss of data, loss of rating or progress, or lost profits arising
            from your use of the Service.
          </p>
        </section>

        <section style={sectionStyle}>
          <h2 style={{ fontSize: '18px', color: '#ffbe5a' }}>7. Contact</h2>
          <p>
            Questions about these terms can be sent through the in-app support channels or to the contact address
            listed in our <Link href="/privacy" style={{ color: '#ffbe5a' }}>Privacy Policy</Link>.
          </p>
        </section>

        <p style={{ marginTop: '40px' }}>
          <Link href="/" style={{ color: '#ffbe5a' }}>← Back to Chess404</Link>
        </p>
      </article>
    </main>
  );
}
