import { useRef, useEffect } from 'react'
import { EditorView, basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { EditorState } from '@codemirror/state'

interface Props {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
}

export default function YamlEditor({ value, onChange, readOnly = false }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)

  useEffect(() => {
    if (!ref.current) return

    const extensions = [
      basicSetup,
      yaml(),
      EditorView.theme({ '&': { height: '400px', border: '1px solid #ccc', borderRadius: '4px' } }),
    ]

    if (readOnly) {
      extensions.push(EditorState.readOnly.of(true))
    }

    if (onChange) {
      extensions.push(
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChange(update.state.doc.toString())
          }
        })
      )
    }

    const view = new EditorView({
      state: EditorState.create({ doc: value, extensions }),
      parent: ref.current,
    })
    viewRef.current = view

    return () => view.destroy()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return <div ref={ref} />
}
