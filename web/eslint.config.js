import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores([
    'dist',
    // Installed from shadcn registries (components.json), not written here —
    // lint findings belong upstream, and a reinstall would overwrite local fixes.
    'src/components/ui',
    'src/components/reui',
    'src/components/animate-ui',
    'src/components/motion',
    'src/components/charts',
    'src/components/unlumen-ui',
    // animate-ui registry dependencies, installed alongside its components.
    'src/hooks/use-controlled-state.tsx',
    'src/hooks/use-is-in-view.tsx',
  ]),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      // `_`-prefixed params/vars are deliberately unused (e.g. braille-loader's
      // shared frame-renderer signature).
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      // ponytail: warn, not error — flags every fetch-in-effect and
      // reset-form-on-open effect (~40 sites). They work; moving them to
      // key-based remounts / a query lib is a refactor, not a lint fix.
      'react-hooks/set-state-in-effect': 'warn',
    },
  },
  {
    // TanStack file routes export only `Route` and define their components
    // locally; the router's Vite plugin handles HMR for that shape.
    files: ['src/routes/**/*.tsx'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
])
