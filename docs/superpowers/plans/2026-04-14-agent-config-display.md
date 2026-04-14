# Agent Configuration Display — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Afficher la configuration active d'un agent depuis la page `AgentDetail` — YAML éditable pour les collectors, labels mis en avant pour les SDK agents.

**Architecture:** Nouveau composant `AgentConfigSection` qui encapsule le fetch de config, les états d'édition et l'appel push. `AgentDetail` est modifié uniquement pour importer et monter ce composant. Aucun changement backend.

**Tech Stack:** React 18, TypeScript, TanStack Query v5, CodeMirror 6 (`YamlEditor` existant), CSS classes du design system Signal Deck existant.

---

## File Map

| Action | Fichier | Rôle |
|--------|---------|------|
| Create | `frontend/src/components/agents/AgentConfigSection.tsx` | Section config : fetch, lecture, édition, push |
| Modify | `frontend/src/pages/AgentDetail.tsx` | Import + montage de `AgentConfigSection` |

---

### Task 1 : Créer `AgentConfigSection.tsx`

**Files:**
- Create: `frontend/src/components/agents/AgentConfigSection.tsx`

- [ ] **Step 1 : Créer le fichier avec le composant complet**

```tsx
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI, agentsAPI } from '../../api/client'
import YamlEditor from '../config/YamlEditor'
import type { Agent } from '../../types'

interface Props {
  agent: Agent
}

export default function AgentConfigSection({ agent }: Props) {
  const queryClient = useQueryClient()
  const [editMode, setEditMode]   = useState(false)
  const [draftYaml, setDraftYaml] = useState('')
  const [pushError, setPushError] = useState<string | null>(null)

  const { data: config, isLoading, isError } = useQuery({
    queryKey: ['agent-config', agent.active_config_id],
    queryFn:  () => configsAPI.get(agent.active_config_id!),
    enabled:  agent.type === 'collector' && !!agent.active_config_id,
  })

  const pushMutation = useMutation({
    mutationFn: () => agentsAPI.pushConfig(agent.id, draftYaml),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-config', agent.active_config_id] })
      setEditMode(false)
      setPushError(null)
    },
    onError: (err: Error) => {
      setPushError(err.message || 'Failed to push configuration')
    },
  })

  function enterEditMode(initialContent: string) {
    setDraftYaml(initialContent)
    setPushError(null)
    setEditMode(true)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setPushError(null)
  }

  // ── SDK agents ──────────────────────────────────────────────────────────
  if (agent.type === 'sdk') {
    const hasLabels = Object.keys(agent.labels).length > 0
    if (!hasLabels) return null

    return (
      <>
        <p className="section-title">Configuration</p>
        <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
          {Object.entries(agent.labels).map(([k, v]) => (
            <span key={k} className="label-chip">
              <span className="label-chip-key">{k}</span>
              <span className="label-chip-eq">=</span>
              <span className="label-chip-val">{v}</span>
            </span>
          ))}
        </div>
      </>
    )
  }

  // ── Collector sans config active ─────────────────────────────────────────
  if (!agent.active_config_id) {
    return (
      <>
        <p className="section-title">Configuration</p>
        {editMode ? (
          <div>
            <YamlEditor value={draftYaml} onChange={setDraftYaml} />
            {pushError && (
              <div className="error-text" style={{ marginTop: '0.5rem' }}>{pushError}</div>
            )}
            <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem' }}>
              <button
                className="btn btn-primary"
                onClick={() => pushMutation.mutate()}
                disabled={!draftYaml || pushMutation.isPending}
              >
                {pushMutation.isPending ? 'Pushing...' : 'Push'}
              </button>
              <button className="btn" onClick={cancelEdit}>Cancel</button>
            </div>
          </div>
        ) : (
          <button className="btn" onClick={() => enterEditMode('')}>Push a config</button>
        )}
      </>
    )
  }

  // ── Collector avec config active ─────────────────────────────────────────
  if (isLoading) {
    return (
      <>
        <p className="section-title">Configuration</p>
        <div className="loading">Loading configuration...</div>
      </>
    )
  }

  if (isError) {
    return (
      <>
        <p className="section-title">Configuration</p>
        <div className="error-text">Impossible de charger la configuration</div>
      </>
    )
  }

  return (
    <>
      <p className="section-title">Configuration</p>
      {editMode ? (
        <div>
          <YamlEditor value={draftYaml} onChange={setDraftYaml} />
          {pushError && (
            <div className="error-text" style={{ marginTop: '0.5rem' }}>{pushError}</div>
          )}
          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem' }}>
            <button
              className="btn btn-primary"
              onClick={() => pushMutation.mutate()}
              disabled={!draftYaml || pushMutation.isPending}
            >
              {pushMutation.isPending ? 'Pushing...' : 'Push'}
            </button>
            <button className="btn" onClick={cancelEdit}>Cancel</button>
          </div>
        </div>
      ) : (
        <div>
          <YamlEditor value={config?.content ?? ''} readOnly />
          <div style={{ marginTop: '0.75rem' }}>
            <button className="btn" onClick={() => enterEditMode(config?.content ?? '')}>
              Edit
            </button>
          </div>
        </div>
      )}
    </>
  )
}
```

- [ ] **Step 2 : Vérifier la compilation TypeScript**

```bash
cd frontend && npx tsc --noEmit
```

Résultat attendu : aucune erreur.

- [ ] **Step 3 : Commit**

```bash
git add frontend/src/components/agents/AgentConfigSection.tsx
git commit -m "feat: add AgentConfigSection component for config display and push"
```

---

### Task 2 : Intégrer `AgentConfigSection` dans `AgentDetail`

**Files:**
- Modify: `frontend/src/pages/AgentDetail.tsx`

- [ ] **Step 1 : Ajouter l'import en haut du fichier**

Dans `frontend/src/pages/AgentDetail.tsx`, ajouter après la ligne d'import `StatusBadge` :

```tsx
import AgentConfigSection from '../components/agents/AgentConfigSection'
```

- [ ] **Step 2 : Monter le composant après la section labels**

Remplacer le bloc de fermeture du composant (actuellement après la section labels) :

Avant :
```tsx
      {/* Labels */}
      {Object.keys(agent.labels).length > 0 && (
        <>
          <p className="section-title">Labels</p>
          <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
            {Object.entries(agent.labels).map(([k, v]) => (
              <span key={k} className="label-chip">
                <span className="label-chip-key">{k}</span>
                <span className="label-chip-eq">=</span>
                <span className="label-chip-val">{v}</span>
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
```

Après :
```tsx
      {/* Labels */}
      {Object.keys(agent.labels).length > 0 && (
        <>
          <p className="section-title">Labels</p>
          <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
            {Object.entries(agent.labels).map(([k, v]) => (
              <span key={k} className="label-chip">
                <span className="label-chip-key">{k}</span>
                <span className="label-chip-eq">=</span>
                <span className="label-chip-val">{v}</span>
              </span>
            ))}
          </div>
        </>
      )}

      {/* Configuration */}
      <AgentConfigSection agent={agent} />
    </div>
  )
}
```

- [ ] **Step 3 : Vérifier la compilation TypeScript**

```bash
cd frontend && npx tsc --noEmit
```

Résultat attendu : aucune erreur.

- [ ] **Step 4 : Vérification manuelle — collector avec config active**

```bash
cd frontend && npm run dev
```

1. Ouvrir `http://localhost:5173/inventory`
2. Cliquer sur un agent de type `collector` qui a une config active
3. Vérifier : section "Configuration" visible, YAML affiché en lecture seule, bouton "Edit" présent
4. Cliquer "Edit" : l'éditeur YAML devient actif, boutons "Push" et "Cancel" apparaissent
5. Modifier le YAML, cliquer "Push" : retour en lecture seule avec le contenu mis à jour
6. Cliquer "Edit" puis "Cancel" : retour en lecture seule sans changement

- [ ] **Step 5 : Vérification manuelle — collector sans config active**

1. Ouvrir la page détail d'un collector sans `active_config_id`
2. Vérifier : section "Configuration" avec bouton "Push a config"
3. Cliquer "Push a config" : éditeur YAML vide, boutons "Push" et "Cancel"
4. Écrire du YAML, cliquer "Push" : requête envoyée, retour état initial

- [ ] **Step 6 : Vérification manuelle — SDK agent**

1. Ouvrir la page détail d'un agent SDK avec des labels
2. Vérifier : section "Configuration" affiche les labels en chips (même rendu que la section Labels)
3. Ouvrir la page détail d'un SDK sans labels : section "Configuration" absente

- [ ] **Step 7 : Commit**

```bash
git add frontend/src/pages/AgentDetail.tsx
git commit -m "feat: display agent configuration in detail page"
```
