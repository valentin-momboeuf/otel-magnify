# Contributing

## Developer Certificate of Origin (DCO)

Tout commit doit être signé avec `--signoff` :

```bash
git commit --signoff -m "feat: description"
```

En signant, tu certifies que ta contribution est conforme au
[DCO 1.1](https://developercertificate.org) — en résumé, que tu as le droit
de soumettre ce code sous la licence du projet.

## Avant de soumettre une PR

Vérifier que les tests passent :

```bash
# Backend
cd backend && go test ./...

# Types frontend
cd frontend && npx tsc --noEmit
```

## Conventions de commit

Format : `type: description` (en anglais)

| Type | Usage |
|------|-------|
| `feat:` | Nouvelle fonctionnalité |
| `fix:` | Correction de bug |
| `docs:` | Documentation uniquement |
| `refactor:` | Refactoring sans changement de comportement |
| `ci:` | CI/CD |

## Licence

En contribuant, tu acceptes que ta contribution soit soumise à la
[Business Source License 1.1](LICENSE) du projet.
