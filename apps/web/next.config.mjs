import path from 'node:path';
import { withSentryConfig } from '@sentry/nextjs';

const nextConfig = {
  transpilePackages: ['@chess404/contracts', '@chess404/game-core'],
  outputFileTracingRoot: path.join(process.cwd(), '../..'),
  async headers() {
    return [
      // Static art and sounds are content the browser can safely keep for a
      // day. Next's default for /public files is max-age=0, which forced a
      // revalidation round-trip for every piece sprite, the background image,
      // and every sound on EVERY page load -- the single biggest perceived
      // sluggishness on repeat visits. Bump to one day (not immutable): a
      // replaced asset becomes visible to returning visitors within 24h.
      { source: '/background.png', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/background.webp', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/pieces/:path*', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/sounds/:path*', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      // Stockfish engine assets are versioned by filename, so they can be
      // cached aggressively (they are multi-MB and were previously re-fetched
      // on every visit under the middleware no-store default).
      ...['/stockfish-18.js', '/stockfish-18-asm.js', '/stockfish-18-single.js', '/stockfish-18-lite.js', '/stockfish-18-lite-single.js', '/stockfish-18-lite.wasm', '/stockfish-18-lite-single.wasm', '/stockfish.js'].map((f) => ({ source: f, headers: [{ key: 'Cache-Control', value: 'public, max-age=31536000, immutable' }] })),
      // Branding + PWA metadata files the middleware matcher no longer
      // no-stores; keep them on the same one-day policy as other art.
      { source: '/logo-mark.png', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/logo192.png', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/logo512.png', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/apple-touch-icon.png', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/favicon.ico', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/manifest.json', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/robots.txt', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
      { source: '/sitemap.xml', headers: [{ key: 'Cache-Control', value: 'public, max-age=86400' }] },
    ];
  },
};

export default withSentryConfig(nextConfig, {
  org: process.env.SENTRY_ORG,
  project: process.env.SENTRY_PROJECT,
  silent: process.env.NODE_ENV === 'production',
  widenClientFileUpload: true,
  hideSourceMaps: true,
  disableLogger: true,
  tunnelRoute: '/monitoring',
});
