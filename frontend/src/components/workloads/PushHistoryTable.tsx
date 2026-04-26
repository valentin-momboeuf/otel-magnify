import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'
import YamlEditor from '../config/YamlEditor'
import type { WorkloadConfig } from '../../types'

interface Props {
  workloadId: string
}

export default function PushHistoryTable({ workloadId }: Props) {
  const queryClient = useQueryClient()
  const [viewing, setViewing] = useState<WorkloadConfig | null>(null)

  const { data: history = [] } = useQuery({
    queryKey: ['workload-config-history', workloadId],
    queryFn: () => workloadsAPI.getConfigHistory(workloadId),
  })

  const rollbackMutation = useMutation({
    mutationFn: (content: string) => workloadsAPI.pushConfig(workloadId, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload', workloadId] })
    },
  })

  if (history.length === 0) return null

  return (
    <>
      <p className="section-title">Push history</p>
      <table className="history-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Status</th>
            <th>User</th>
            <th>Hash</th>
            <th>Error</th>
            <th aria-label="actions"></th>
          </tr>
        </thead>
        <tbody>
          {history.map((row) => (
            <tr key={`${row.config_id}-${row.applied_at}`}>
              <td>{new Date(row.applied_at).toLocaleString()}</td>
              <td>
                <span className={`status-pill status-${row.status}`}>{row.status}</span>
              </td>
              <td>{row.pushed_by || '—'}</td>
              <td>
                <code>{row.config_id.substring(0, 8)}</code>
              </td>
              <td className="history-error">{row.error_message || ''}</td>
              <td>
                <button className="btn btn-small" onClick={() => setViewing(row)}>
                  View
                </button>
                {row.status === 'applied' && row.content && (
                  <button
                    className="btn btn-small"
                    onClick={() => rollbackMutation.mutate(row.content!)}
                    disabled={rollbackMutation.isPending}
                  >
                    Rollback to this
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {viewing && (
        <div className="modal-backdrop" onClick={() => setViewing(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <span>Config {viewing.config_id.substring(0, 12)}</span>
              <button className="btn btn-small" onClick={() => setViewing(null)}>
                Close
              </button>
            </div>
            <YamlEditor value={viewing.content ?? ''} readOnly />
          </div>
        </div>
      )}
    </>
  )
}
