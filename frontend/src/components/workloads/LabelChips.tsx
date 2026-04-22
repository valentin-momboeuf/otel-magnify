interface Props {
  labels?: Record<string, string>
  max?: number
}

export default function LabelChips({ labels, max = 4 }: Props) {
  const entries = Object.entries(labels ?? {})
  if (entries.length === 0) return null

  const visible = entries.slice(0, max)
  const extra = entries.length - visible.length

  return (
    <div className="label-chips">
      {visible.map(([k, v]) => (
        <span key={k} className="label-chip">
          <span className="label-chip-key">{k}</span>
          <span className="label-chip-eq">=</span>
          <span className="label-chip-val">{v}</span>
        </span>
      ))}
      {extra > 0 && <span className="label-chip label-chip-extra">+{extra}</span>}
    </div>
  )
}
