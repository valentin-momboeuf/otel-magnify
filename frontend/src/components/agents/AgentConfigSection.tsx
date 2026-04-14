import { useState } from 'react'
import axios from 'axios'
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
      // Invalidating the agent causes a re-render with the updated active_config_id,
      // which in turn triggers a fresh config query with the new key.
      queryClient.invalidateQueries({ queryKey: ['agent', agent.id] })
      setEditMode(false)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to push configuration'
      setPushError(msg)
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
