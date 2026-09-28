// ESLint flat config for the web app. typescript-eslint provides the TS
// parser/project awareness, and @next/eslint-plugin-next's flatConfig
// presets (recommended + coreWebVitals) apply the same rule set Next's own
// `next lint` would.
//
// Gating policy: app/** (API routes + layouts) and src/lib/** (service
// clients) are held at zero warnings -- they are the security-sensitive
// surface every recent hardening change touches. Legacy UI code
// (src/hooks, src/components, src/*.tsx) predates any linting and still
// carries thousands of pre-existing findings; every rule from the
// typescript-eslint and @next/next plugins is downgraded to a warning there
// so `pnpm run lint` gates the sensitive surface today without a mass
// rewrite, while keeping the debt visible in every lint run until it is
// paid down directory by directory.
import nextPlugin from '@next/eslint-plugin-next';
import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

const { flatConfig } = nextPlugin;

// Downgrade every rule a plugin ships, not a hand-picked list: newly
// enabled rules then auto-downgrade in legacy code instead of breaking the
// gate. Type-aware rules are skipped: they would demand a full TS project
// pass (slow) and are never enabled by the recommended presets below.
const downgradeAll = (prefix, plugin) =>
  Object.fromEntries(
    Object.entries(plugin.rules ?? {})
      .filter(([, rule]) => !rule?.meta?.docs?.requiresTypeChecking)
      .map(([rule]) => [`${prefix}/${rule}`, 'warn']),
  );

const strictSurface = {
  files: ['app/**/*.{ts,tsx}', 'src/lib/**/*.{ts,tsx}'],
};

// react-hooks/rules-of-hooks stays an ERROR everywhere (it catches real
// bugs); exhaustive-deps is a warning in legacy UI code only.
const reactHooksConfig = {
  files: ['**/*.{ts,tsx}'],
  plugins: { 'react-hooks': reactHooks },
  rules: {
    'react-hooks/rules-of-hooks': 'error',
    'react-hooks/exhaustive-deps': 'warn',
  },
};

const legacySurface = {
  files: ['src/**/*.{ts,tsx}'],
  // src/lib is service-client code and stays strict; everything else under
  // src (hooks, components, page components) is the legacy debt surface.
  ignores: ['src/lib/**'],
  rules: {
    ...downgradeAll('@typescript-eslint', tseslint.plugin),
    ...downgradeAll('@next/next', nextPlugin),
    'react-hooks/exhaustive-deps': 'warn',
  },
};

const testOverrides = {
  files: ['**/*.test.{ts,tsx}'],
  rules: {
    '@typescript-eslint/no-explicit-any': 'off',
    '@typescript-eslint/no-unused-vars': 'off',
  },
};

export default tseslint.config(
  { ignores: ['.next/**', 'node_modules/**', 'playwright-report/**', 'test-results/**', 'public/**', 'next-env.d.ts'] },
  tseslint.configs.recommended,
  flatConfig.recommended,
  flatConfig.coreWebVitals,
  reactHooksConfig,
  strictSurface,
  legacySurface,
  testOverrides,
);
