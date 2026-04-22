import { useEffect, useRef } from 'react'
import { MergeView } from '@codemirror/merge'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { signalDeckYaml } from '../config/yamlTheme'

interface Props {
  oldYaml: string
  newYaml: string
}

export default function ConfigDiffView({ oldYaml, newYaml }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<MergeView | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const extensions = [
      basicSetup,
      yaml(),
      signalDeckYaml,
      EditorState.readOnly.of(true),
      EditorView.theme({ '&': { height: '400px' } }),
    ]
    const mv = new MergeView({
      a: { doc: oldYaml, extensions },
      b: { doc: newYaml, extensions },
      parent: ref.current,
    })
    viewRef.current = mv
    return () => {
      mv.destroy()
      viewRef.current = null
    }
  }, [oldYaml, newYaml])

  return <div ref={ref} className="config-diff-view" />
}
