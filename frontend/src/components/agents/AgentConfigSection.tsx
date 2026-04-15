import { useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI, agentsAPI } from '../../api/client'
import YamlEditor from '../config/YamlEditor'
import type { Agent, ValidationResult } from '../../types'

interface Props {
  agent: Agent
}

export default function AgentConfigSection({ agent }: Props) {
  const queryClient = useQueryClient()
  const [editMode, setEditMode]       = useState(false)
  const [draftYaml, setDraftYaml]     = useState('')
  const [pushError, setPushError]     = useState<string | null>(null)
  const [validation, setValidation]   = useState<ValidationResult | null>(null)

  const { data: config, isLoading, isError } = useQuery({
    queryKey: ['agent-config', agent.active_config_id],
    queryFn:  () => configsAPI.get(agent.active_config_id!),
    enabled:  agent.type === 'collector' && !!agent.active_config_id,
  })

  const validateMutation = useMutation({
    mutationFn: () => agentsAPI.validateConfig(agent.id, draftYaml),
    onSuccess: (result) => {
      setValidation(result)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Validation request failed'
      setPushError(msg)
    },
  })

  const pushMutation = useMutation({
    mutationFn: () => agentsAPI.pushConfig(agent.id, draftYaml),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent', agent.id] })
      setEditMode(false)
      setValidation(null)
      setPushError(null)
    },
    onError: (err: unknown) => {
      if (axios.isAxiosError(err) && err.response?.data?.validation_errors) {
        setValidation({
          valid: false,
          errors: err.response.data.validation_errors,
        })
        setPushError('Configuration failed validation')
        return
      }
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to push configuration'
      setPushError(msg)
    },
  })

  function enterEditMode(initialContent: string) {
    setDraftYaml(initialContent)
    setPushError(null)
    setValidation(null)
    setEditMode(true)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setPushError(null)
    setValidation(null)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    if (validation !== null) {
      setValidation(null) // invalidate previous validation when user edits
    }
  }

  const canPush =
    !!draftYaml &&
    !pushMutation.isPending &&
    validation !== null &&
    validation.valid === true

  const editorPanel = (
    <div>
      <YamlEditor value={draftYaml} onChange={onDraftChange} />

      {validation && (
        <div
          className={validation.valid ? 'validation-ok' : 'validation-errors'}
          style={{ marginTop: '0.5rem' }}
        >
          {validation.valid ? (
            <span>✓ Configuration is valid</span>
          ) : (
            <ul style={{ margin: 0, paddingLeft: '1.25rem' }}>
              {(validation.errors ?? []).map((e, i) => (
                <li key={i}>
                  <strong>{e.code}</strong>
                  {e.path ? <code style={{ marginLeft: 6 }}>{e.path}</code> : null}
                  <span style={{ marginLeft: 6 }}>— {e.message}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {pushError && (
        <div className="error-text" style={{ marginTop: '0.5rem' }}>{pushError}</div>
      )}

      <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem' }}>
        <button
          className="btn"
          onClick={() => validateMutation.mutate()}
          disabled={!draftYaml || validateMutation.isPending}
          title="Check the configuration against installed collector components"
        >
          {validateMutation.isPending ? 'Validating...' : 'Validate'}
        </button>
        <button
          className="btn btn-primary"
          onClick={() => pushMutation.mutate()}
          disabled={!canPush}
          title={
            validation === null
              ? 'Validate the configuration first'
              : !validation.valid
                ? 'Fix validation errors before pushing'
                : ''
          }
        >
          {pushMutation.isPending ? 'Pushing...' : 'Push'}
        </button>
        <button className="btn" onClick={cancelEdit}>Cancel</button>
      </div>
    </div>
  )

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

  // ── Collector without active config ──────────────────────────────────────
  if (!agent.active_config_id) {
    return (
      <>
        <p className="section-title">Configuration</p>
        {editMode ? editorPanel : (
          <button className="btn" onClick={() => enterEditMode('')}>Push a config</button>
        )}
      </>
    )
  }

  // ── Collector with active config ─────────────────────────────────────────
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
        <div className="error-text">Failed to load configuration</div>
      </>
    )
  }

  return (
    <>
      <p className="section-title">Configuration</p>
      {editMode ? editorPanel : (
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
