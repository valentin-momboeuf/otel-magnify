# otel-magnify frontend

React 19 + TypeScript + Vite. The frontend is embedded into the Go binary via `embed.FS` for production deployment — see the parent [README](../README.md) and [CLAUDE.md](../CLAUDE.md) for the overall architecture.

## Local development

```bash
# install deps
npm ci

# dev server (Vite, HMR)
npm run dev

# type check + production build
npm run build

# lint and format
npm run lint
npm run format:check

# E2E tests (requires backend running on the expected port)
npm run test:e2e
npm run test:e2e:real    # against a real backend
```

## Project conventions

- ESLint flat config with `eslint-plugin-security` and `eslint-plugin-react-hooks` v7
- Prettier for formatting (config: `.prettierrc.json` at this directory)
- All conventions and architecture decisions live in the parent [`CLAUDE.md`](../CLAUDE.md)
