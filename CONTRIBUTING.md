# Contributing

## Developer Certificate of Origin (DCO)

Every commit must be signed off with `--signoff`:

```bash
git commit --signoff -m "feat: description"
```

By signing off, you certify that your contribution complies with the
[DCO 1.1](https://developercertificate.org) — in short, that you have the right
to submit this code under the project's license.

## Before submitting a PR

Make sure the tests pass:

```bash
# Backend
cd backend && go test ./...

# Frontend types
cd frontend && npx tsc --noEmit
```

## Commit conventions

Format: `type: description` (in English)

| Type | Usage |
|------|-------|
| `feat:` | New feature |
| `fix:` | Bug fix |
| `docs:` | Documentation only |
| `refactor:` | Refactoring without behavior change |
| `ci:` | CI/CD |

## License

By contributing, you agree that your contribution will be licensed under
the project's [Business Source License 1.1](LICENSE).
