import { useEffect, useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI, workloadsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import PushHistoryTable from './PushHistoryTable'
import { useStore } from '../../store'
import { isReadOnlyCollector } from '../../lib/workloadCapabilities'
import type { Workload, ValidationResult } from '../../types'

interface Props {
  workload: Workload
}

type Tab = 'edit' | 'diff'

const PUSH_TIMEOUT_MS = 30_000

export default function WorkloadConfigSection({ workload }: Props) {
  const queryClient = useQueryClient()
  const configStatus = useStore((s) => s.configStatus[workload.id])
  const rollback = useStore((s) => s.lastRollback[workload.id])
  const clearRollback = useStore((s) => s.clearAutoRollback)

  const [editMode, setEditMode] = useState(false)
  const [tab, setTab] = useState<Tab>('edit')
  const [draftYaml, setDraftYaml] = useState('')
  const [pendingHash, setPendingHash] = useState<string | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [pushError, setPushError] = useState<string | null>(null)
  const [selectedConfigId, setSelectedConfigId] = useState('')

  const {
    data: config,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ['workload-config', workload.active_config_id],
    queryFn: () => configsAPI.get(workload.active_config_id!),
    enabled: workload.type === 'collector' && !!workload.active_config_id,
  })

  const { data: savedConfigs, isError: configsListError } = useQuery({
    queryKey: ['configs'],
    queryFn: configsAPI.list,
    retry: false,
  })

  const activeContent = config?.content ?? ''

  const validateMutation = useMutation({
    mutationFn: () => workloadsAPI.validateConfig(workload.id, draftYaml),
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
    mutationFn: () => workloadsAPI.pushConfig(workload.id, draftYaml),
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

  const loadConfigMutation = useMutation({
    mutationFn: (configId: string) => configsAPI.get(configId),
    onSuccess: (cfg) => {
      enterEditMode(cfg.content, workload.active_config_id ? 'diff' : 'edit')
      setSelectedConfigId('')
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to load configuration'
      setPushError(`Failed to load configuration: ${msg}`)
      setSelectedConfigId('')
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
      queryClient.invalidateQueries({ queryKey: ['workload', workload.id] })
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workload.id] })
    } else if (configStatus.status === 'failed') {
      setPendingHash(null)
      setTimedOut(false)
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workload.id] })
      // keep editMode + draftYaml so the user can fix and retry
    }
  }, [configStatus, pendingHash, workload.id, queryClient])

  useEffect(() => {
    if (!pendingHash) return
    const timer = setTimeout(() => setTimedOut(true), PUSH_TIMEOUT_MS)
    return () => clearTimeout(timer)
  }, [pendingHash])

  function enterEditMode(initialContent: string, targetTab: Tab = 'edit') {
    setDraftYaml(initialContent)
    setEditMode(true)
    setTab(targetTab)
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
    ? {
        status: 'applying' as const,
        config_hash: pendingHash,
        updated_at: new Date().toISOString(),
      }
    : configStatus

  const canPush =
    !!draftYaml &&
    !pendingHash &&
    !pushMutation.isPending &&
    validation !== null &&
    validation.valid === true

  // ── SDK workloads: labels as "configuration" ──────────────────────────────
  if (workload.type === 'sdk') {
    const hasLabels = Object.keys(workload.labels).length > 0
    if (!hasLabels) return null
    return (
      <>
        <p className="section-title">Configuration</p>
        <div className="label-chip-list">
          {Object.entries(workload.labels).map(([k, v]) => (
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
  if (isReadOnlyCollector(workload)) {
    const hasConfig = !!workload.active_config_id
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
          Read-only — this collector uses the <code>opamp</code> extension which can only report its
          config. Run it under the OpAMP Supervisor to enable config push.{' '}
          <a
            href={`${DOCS_BASE_URL}/users/connecting-agents.md#running-a-collector-via-opamp-supervisor`}
            target="_blank"
            rel="noreferrer"
          >
            Learn more →
          </a>
        </div>
        <PushHistoryTable workloadId={workload.id} />
      </>
    )
  }

  const editorPanel = (
    <div>
      <div className="tabstrip">
        <button
          className={`tab ${tab === 'edit' ? 'tab-active' : ''}`}
          onClick={() => setTab('edit')}
        >
          Edit
        </button>
        <button
          className={`tab ${tab === 'diff' ? 'tab-active' : ''}`}
          onClick={() => setTab('diff')}
          disabled={!workload.active_config_id}
          title={workload.active_config_id ? '' : 'No active config to diff against'}
        >
          Diff
        </button>
      </div>

      {tab === 'edit' && <YamlEditor value={draftYaml} onChange={onDraftChange} />}
      {tab === 'diff' && <ConfigDiffView oldYaml={activeContent} newYaml={draftYaml} />}

      {validation && (
        <div
          className={`validation-block ${validation.valid ? 'validation-ok' : 'validation-errors'}`}
        >
          {validation.valid ? (
            <span>✓ Configuration is valid</span>
          ) : (
            <ul className="validation-error-list">
              {(validation.errors ?? []).map((e, i) => (
                <li key={i}>
                  <strong>{e.code}</strong>
                  {e.path ? <code className="validation-error-path">{e.path}</code> : null}
                  <span className="validation-error-msg">— {e.message}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {pushError && <div className="error-text error-text-push">{pushError}</div>}

      <div className="btn-row">
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
        <button className="btn" onClick={cancelEdit} disabled={!!pendingHash}>
          Cancel
        </button>
        {timedOut && (
          <span className="error-text error-text-inline">
            No response from workload — still applying?
          </span>
        )}
      </div>
    </div>
  )

  const isConfigsEmpty = !configsListError && (savedConfigs?.length ?? 0) === 0
  let placeholderLabel = '— Apply a saved config —'
  if (configsListError) {
    placeholderLabel = '— Failed to load configs —'
  } else if (isConfigsEmpty) {
    placeholderLabel = '— No saved configs (create one in Configs) —'
  }

  const applySelector = (
    <select
      className="filter-select apply-config-select"
      value={selectedConfigId}
      onChange={(e) => {
        const id = e.target.value
        if (!id) return
        setSelectedConfigId(id)
        loadConfigMutation.mutate(id)
      }}
      aria-label="Apply a saved config"
      disabled={loadConfigMutation.isPending || !!pendingHash || isConfigsEmpty || configsListError}
    >
      <option value="">{placeholderLabel}</option>
      {(savedConfigs ?? []).map((c) => (
        <option key={c.id} value={c.id}>
          {c.id === workload.active_config_id ? `${c.name} (currently applied)` : c.name}
        </option>
      ))}
    </select>
  )

  // ── Collector without active config ──────────────────────────────────────
  if (!workload.active_config_id) {
    return (
      <>
        <p className="section-title">Configuration</p>
        {applySelector}
        {editMode ? (
          editorPanel
        ) : (
          <button className="btn" onClick={() => enterEditMode('')}>
            Push a config
          </button>
        )}
        <PushStatusBanner
          status={derivedStatus}
          rollback={rollback}
          onDismissRollback={() => clearRollback(workload.id)}
        />
        <PushHistoryTable workloadId={workload.id} />
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
      {applySelector}

      {!editMode ? (
        <div>
          <YamlEditor value={activeContent} readOnly />
          <div className="btn-row btn-row-top">
            <button className="btn" onClick={() => enterEditMode(activeContent)}>
              Edit
            </button>
          </div>
        </div>
      ) : (
        editorPanel
      )}

      <PushStatusBanner
        status={derivedStatus}
        rollback={rollback}
        onDismissRollback={() => clearRollback(workload.id)}
      />

      <PushHistoryTable workloadId={workload.id} />
    </>
  )
}
