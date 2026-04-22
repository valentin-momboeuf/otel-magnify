import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { workloadsAPI } from '../api/client'
import WorkloadCard from '../components/workloads/WorkloadCard'
import { isSupervised, isReadOnlyCollector } from '../lib/workloadCapabilities'

type ControlFilter = '' | 'supervised' | 'readonly'

export default function Inventory() {
  const { data: workloads, isLoading } = useQuery({
    queryKey: ['workloads'],
    queryFn: () => workloadsAPI.list(),
  })
  const [searchParams] = useSearchParams()

  const [filterType,    setFilterType]    = useState<string>(searchParams.get('type') ?? '')
  const [filterStatus,  setFilterStatus]  = useState<string>('')
  const [filterControl, setFilterControl] = useState<ControlFilter>(
    (searchParams.get('control') as ControlFilter) ?? ''
  )

  useEffect(() => {
    const type = searchParams.get('type')
    if (type) setFilterType(type)
    const control = searchParams.get('control') as ControlFilter | null
    if (control) setFilterControl(control)
  }, [searchParams])

  const filtered = (workloads ?? []).filter((w) => {
    if (filterType   && w.type   !== filterType)   return false
    if (filterStatus && w.status !== filterStatus) return false
    if (filterControl) {
      if (w.type !== 'collector')                                    return false
      if (filterControl === 'supervised' && !isSupervised(w))        return false
      if (filterControl === 'readonly'   && !isReadOnlyCollector(w)) return false
    }
    return true
  })

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Inventory</h1>
        <span className="page-header-count">
          {filtered.length} / {workloads?.length ?? 0}
        </span>
      </div>

      <div className="filter-bar">
        <select
          className="filter-select"
          value={filterType}
          onChange={(e) => setFilterType(e.target.value)}
        >
          <option value="">All types</option>
          <option value="collector">collector</option>
          <option value="sdk">sdk</option>
        </select>
        <select
          className="filter-select"
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
        >
          <option value="">All statuses</option>
          <option value="connected">connected</option>
          <option value="disconnected">disconnected</option>
          <option value="degraded">degraded</option>
        </select>
        <select
          className="filter-select"
          value={filterControl}
          onChange={(e) => setFilterControl(e.target.value as ControlFilter)}
        >
          <option value="">All control</option>
          <option value="supervised">supervised</option>
          <option value="readonly">read-only</option>
        </select>
      </div>

      {isLoading ? (
        <div className="loading">Loading workloads...</div>
      ) : filtered.length === 0 ? (
        <div className="empty-state">No workloads match the current filter</div>
      ) : (
        filtered.map((w) => <WorkloadCard key={w.id} workload={w} />)
      )}
    </div>
  )
}
