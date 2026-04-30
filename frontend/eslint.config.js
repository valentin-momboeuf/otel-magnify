import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import security from 'eslint-plugin-security'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'playwright-report', 'test-results']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
      security.configs.recommended,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Disabled: fires on every legitimate dynamic property access (e.g. acc[day],
      // map[status], updated[idx]). All flagged instances are false positives — keys
      // are derived from typed API responses or internal accumulators, not user input.
      'security/detect-object-injection': 'off',
    },
  },
  {
    // Playwright fixtures use `use(...)` which the lint rule misidentifies as a
    // React Hook. The pattern is documented Playwright API, not a React Hook.
    files: ['tests/**/*.ts'],
    rules: {
      'react-hooks/rules-of-hooks': 'off',
    },
  },
])
