import { useQuery } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'

interface Props {
  workloadId: string
  activeConfigHash?: string
}

export default function InstancesTab({ workloadId, activeConfigHash }: Props) {
  const { data: instances, isLoading } = useQuery({
    queryKey: ['workload-instances', workloadId],
    queryFn: () => workloadsAPI.instances(workloadId),
    refetchInterval: 5000,
  })

  if (isLoading) {
    return <div className="loading">Loading instances…</div>
  }
  if (!instances || instances.length === 0) {
    return <div className="empty-state">No instance currently connected</div>
  }

  return (
    <table className="instances-table">
      <thead>
        <tr>
          <th>Instance</th>
          <th>Pod</th>
          <th>Version</th>
          <th>Connected</th>
          <th>Effective config</th>
        </tr>
      </thead>
      <tbody>
        {instances.map((i) => {
          const drift = Boolean(
            activeConfigHash &&
            i.effective_config_hash &&
            i.effective_config_hash !== activeConfigHash,
          )
          return (
            <tr key={i.instance_uid} className={drift ? 'instance-drift' : undefined}>
              <td className="mono">{i.instance_uid.slice(0, 8)}</td>
              <td>{i.pod_name || '—'}</td>
              <td>{i.version || '—'}</td>
              <td>{new Date(i.connected_at).toLocaleTimeString()}</td>
              <td className="mono">
                {i.effective_config_hash ? i.effective_config_hash.slice(0, 8) : '—'}
                {drift && <span className="instance-drift-tag"> drift</span>}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
