# Spec — Agent Configuration Display

**Date:** 2026-04-14  
**Statut:** approved

## Contexte

La page `AgentDetail` (`/inventory/:id`) affiche les métadonnées d'un agent (type, version, status, labels). Elle ne montre pas le contenu de la configuration active. L'objectif est d'ajouter une section "Configuration" visible directement depuis cette page.

## Périmètre

- Affichage de la configuration active pour les collectors (YAML)
- Affichage des attributs/labels comme configuration pour les SDK agents
- Édition inline et push de config pour les collectors
- Aucune modification du backend

## Architecture

### Nouveau composant

`frontend/src/components/agents/AgentConfigSection.tsx`

Reçoit en props l'objet `Agent` complet. Gère en interne le fetch de la config, les états d'édition et l'appel push.

### Modification existante

`frontend/src/pages/AgentDetail.tsx` — ajout de `<AgentConfigSection agent={agent} />` après la section labels. Aucun autre changement.

## Comportement par type

### Collector (`agent.type === 'collector'`)

**Cas 1 — `active_config_id` présent :**

1. `useQuery(['agent-config', active_config_id])` fetche `GET /api/configs/{id}`
2. Affichage lecture seule du YAML dans un bloc de code
3. Bouton "Edit" → bascule `editMode = true` : le bloc est remplacé par `YamlEditor` (composant existant), `draftYaml` initialisé avec le contenu actif
4. Bouton "Push" → appel `agentsAPI.pushConfig(agent.id, draftYaml)`, puis invalidation du cache `['agent-config', active_config_id]`, retour en mode lecture seule
5. Bouton "Cancel" → retour en mode lecture seule sans appel API

**Cas 2 — pas de `active_config_id` :**

Section vide avec un bouton "Push a config" qui ouvre directement `YamlEditor` avec du YAML vide.

### SDK (`agent.type === 'sdk'`)

Affichage des labels de l'agent sous un titre "Configuration", dans le même format que la section labels actuelle. La section labels existante dans `AgentDetail` reste en place.

## États locaux

| État | Type | Rôle |
|------|------|------|
| `editMode` | `boolean` | Bascule lecture / écriture |
| `draftYaml` | `string` | Copie locale du YAML pendant l'édition |
| `isPushing` | `boolean` | Désactive le bouton Push pendant l'appel API |
| `pushError` | `string \| null` | Message d'erreur affiché sous l'éditeur |

## Gestion d'erreurs

| Cas | Comportement |
|-----|-------------|
| Fetch config échoue | Message inline "Impossible de charger la configuration", le reste de la page reste fonctionnel |
| Push échoue | `pushError` affiché sous l'éditeur, on reste en `editMode` pour permettre correction/retry |
| Push réussit | Invalidation du cache `['agent-config', ...]`, refetch automatique, retour en mode lecture seule |

## Data flow

```
AgentDetail
  └── AgentConfigSection (props: agent)
        ├── [collector] useQuery('agent-config', active_config_id)
        │     └── GET /api/configs/{id}  →  Config.content (YAML)
        ├── [collector, editMode] YamlEditor + agentsAPI.pushConfig()
        └── [sdk] affichage des agent.labels
```

## Ce qui n'est pas couvert

- Historique des configurations (liste des configs précédentes) — hors périmètre
- Diff entre config active et config poussée — hors périmètre
- Tests unitaires frontend — pas de suite de tests React en place
