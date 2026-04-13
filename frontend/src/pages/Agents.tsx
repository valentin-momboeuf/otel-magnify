import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { agentsAPI } from '../api/client'
import AgentCard from '../components/agents/AgentCard'

export default function Inventory() {
  const { data: agents, isLoading } = useQuery({ queryKey: ['agents'], queryFn: agentsAPI.list })
  const [searchParams] = useSearchParams()

  const [filterType,   setFilterType]   = useState<string>(searchParams.get('type') ?? '')
  const [filterStatus, setFilterStatus] = useState<string>('')

  // Sync filter when navigating from Dashboard stat cards
  useEffect(() => {
    const type = searchParams.get('type')
    if (type) setFilterType(type)
  }, [searchParams])

  const filtered = (agents ?? []).filter((a) => {
    if (filterType   && a.type   !== filterType)   return false
    if (filterStatus && a.status !== filterStatus) return false
    return true
  })

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Inventory</h1>
        <span style={{ fontFamily: 'var(--mono)', fontSize: '0.7rem', color: 'var(--muted)' }}>
          {filtered.length} / {agents?.length ?? 0}
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
      </div>

      {isLoading ? (
        <div className="loading">Loading agents...</div>
      ) : filtered.length === 0 ? (
        <div className="empty-state">No agents match the current filter</div>
      ) : (
        filtered.map((a) => <AgentCard key={a.id} agent={a} />)
      )}
    </div>
  )
}
