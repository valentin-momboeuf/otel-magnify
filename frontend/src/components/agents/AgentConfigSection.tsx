import { useEffect, useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI, agentsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import PushHistoryTable from './PushHistoryTable'
import { useStore } from '../../store'
import { isReadOnlyCollector } from '../../lib/agentCapabilities'
import type { Agent, ValidationResult } from '../../types'

interface Props {
  agent: Agent
}

type Tab = 'edit' | 'diff'

const PUSH_TIMEOUT_MS = 30_000

export default function AgentConfigSection({ agent }: Props) {
  const queryClient = useQueryClient()
  const configStatus = useStore((s) => s.configStatus[agent.id])
  const rollback = useStore((s) => s.lastRollback[agent.id])
  const clearRollback = useStore((s) => s.clearAutoRollback)

  const [editMode, setEditMode]       = useState(false)
  const [tab, setTab]                 = useState<Tab>('edit')
  const [draftYaml, setDraftYaml]     = useState('')
  const [pendingHash, setPendingHash] = useState<string | null>(null)
  const [timedOut, setTimedOut]       = useState(false)
  const [validation, setValidation]   = useState<ValidationResult | null>(null)
  const [pushError, setPushError]     = useState<string | null>(null)

  const { data: config, isLoading, isError } = useQuery({
    queryKey: ['agent-config', agent.active_config_id],
    queryFn: () => configsAPI.get(agent.active_config_id!),
    enabled: agent.type === 'collector' && !!agent.active_config_id,
  })

  const activeContent = config?.content ?? ''

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
    onSuccess: (res) => {
      setPendingHash(res.config_hash)
      setTimedOut(false)
      setPushError(null)
    },
    onError: (err: unknown) => {
      if (axios.isAxiosError(err) && err.response?.data?.validation_errors) {
        setValidation({ valid: false, errors: err.response.data.validation_errors })
        setPushError('Configuration failed validation')
        return
      }
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to push configuration'
      setPushError(msg)
    },
  })

  // React to WS-driven status updates that match our pending push hash.
  useEffect(() => {
    if (!pendingHash || !configStatus) return
    if (configStatus.config_hash !== pendingHash) return
    if (configStatus.status === 'applied') {
      setPendingHash(null)
      setTimedOut(false)
      setEditMode(false)
      setDraftYaml('')
      setValidation(null)
      queryClient.invalidateQueries({ queryKey: ['agent', agent.id] })
      queryClient.invalidateQueries({ queryKey: ['agent-config-history', agent.id] })
    } else if (configStatus.status === 'failed') {
      setPendingHash(null)
      setTimedOut(false)
      queryClient.invalidateQueries({ queryKey: ['agent-config-history', agent.id] })
      // keep editMode + draftYaml so the user can fix and retry
    }
  }, [configStatus, pendingHash, agent.id, queryClient])

  useEffect(() => {
    if (!pendingHash) return
    const timer = setTimeout(() => setTimedOut(true), PUSH_TIMEOUT_MS)
    return () => clearTimeout(timer)
  }, [pendingHash])

  function enterEditMode(initialContent: string) {
    setDraftYaml(initialContent)
    setEditMode(true)
    setTab('edit')
    setValidation(null)
    setPushError(null)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setValidation(null)
    setPushError(null)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    if (validation !== null) setValidation(null)
  }

  const derivedStatus = pendingHash
    ? { status: 'applying' as const, config_hash: pendingHash, updated_at: new Date().toISOString() }
    : configStatus

  const canPush =
    !!draftYaml &&
    !pendingHash &&
    !pushMutation.isPending &&
    validation !== null &&
    validation.valid === true

  // ── SDK agents: labels as "configuration" ──────────────────────────────
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

  // ── Collector that does not accept remote config: read-only view ─────────
  if (isReadOnlyCollector(agent)) {
    const hasConfig = !!agent.active_config_id
    return (
      <>
        <p className="section-title">Configuration</p>
        {hasConfig && isLoading ? (
          <div className="loading">Loading configuration...</div>
        ) : hasConfig && isError ? (
          <div className="error-text">Failed to load configuration</div>
        ) : hasConfig ? (
          <YamlEditor value={activeContent} readOnly />
        ) : (
          <div className="empty-state">No config reported yet.</div>
        )}
        <div className="config-readonly-note">
          Read-only — this collector uses the <code>opamp</code> extension which can only report its config. Run it under the OpAMP Supervisor to enable config push.{' '}
          <a
            href={`${DOCS_BASE_URL}/users/connecting-agents.md#running-a-collector-via-opamp-supervisor`}
            target="_blank"
            rel="noreferrer"
          >
            Learn more →
          </a>
        </div>
        <PushHistoryTable agentId={agent.id} />
      </>
    )
  }

  const editorPanel = (
    <div>
      <div className="tabstrip">
        <button className={`tab ${tab === 'edit' ? 'tab-active' : ''}`} onClick={() => setTab('edit')}>Edit</button>
        <button
          className={`tab ${tab === 'diff' ? 'tab-active' : ''}`}
          onClick={() => setTab('diff')}
          disabled={!agent.active_config_id}
          title={agent.active_config_id ? '' : 'No active config to diff against'}
        >
          Diff
        </button>
      </div>

      {tab === 'edit' && <YamlEditor value={draftYaml} onChange={onDraftChange} />}
      {tab === 'diff' && <ConfigDiffView oldYaml={activeContent} newYaml={draftYaml} />}

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
          disabled={!draftYaml || validateMutation.isPending || !!pendingHash}
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
          {pendingHash ? 'Applying...' : pushMutation.isPending ? 'Pushing...' : 'Push'}
        </button>
        <button className="btn" onClick={cancelEdit} disabled={!!pendingHash}>Cancel</button>
        {timedOut && (
          <span className="error-text" style={{ alignSelf: 'center' }}>
            No response from agent — still applying?
          </span>
        )}
      </div>
    </div>
  )

  // ── Collector without active config ──────────────────────────────────────
  if (!agent.active_config_id) {
    return (
      <>
        <p className="section-title">Configuration</p>
        {editMode ? editorPanel : (
          <button className="btn" onClick={() => enterEditMode('')}>Push a config</button>
        )}
        <PushStatusBanner
          status={derivedStatus}
          rollback={rollback}
          onDismissRollback={() => clearRollback(agent.id)}
        />
        <PushHistoryTable agentId={agent.id} />
      </>
    )
  }

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

      {!editMode ? (
        <div>
          <YamlEditor value={activeContent} readOnly />
          <div style={{ marginTop: '0.75rem' }}>
            <button className="btn" onClick={() => enterEditMode(activeContent)}>Edit</button>
          </div>
        </div>
      ) : editorPanel}

      <PushStatusBanner
        status={derivedStatus}
        rollback={rollback}
        onDismissRollback={() => clearRollback(agent.id)}
      />

      <PushHistoryTable agentId={agent.id} />
    </>
  )
}
