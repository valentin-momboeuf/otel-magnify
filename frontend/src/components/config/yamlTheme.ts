import { EditorView } from '@codemirror/view'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

const accentGold = '#d4a84a'
const sand = '#c9b580'
const muted = '#6b6a63'
const accentSecondary = '#8c9eb6'
const fg = '#e5e2d7'
const bg = '#14130f'

const highlight = HighlightStyle.define([
  { tag: [t.propertyName, t.definition(t.propertyName)], color: accentGold, fontWeight: '500' },
  { tag: [t.string, t.special(t.string)], color: sand },
  { tag: t.comment, color: muted, fontStyle: 'italic' },
  { tag: [t.number, t.bool, t.null], color: accentSecondary },
  { tag: t.keyword, color: accentSecondary },
  { tag: t.meta, color: accentSecondary },
  { tag: t.invalid, color: '#e06c75' },
])

const theme = EditorView.theme({
  '&': { color: fg, backgroundColor: bg, fontSize: '0.8rem' },
  '.cm-content': { fontFamily: 'var(--mono, Fira Code, monospace)', caretColor: accentGold },
  '.cm-cursor': { borderLeftColor: accentGold },
  '&.cm-focused .cm-selectionBackground, ::selection': { backgroundColor: '#3a3424' },
  '.cm-gutters': { backgroundColor: '#0f0e0b', color: muted, border: 'none' },
  '.cm-activeLine': { backgroundColor: '#1b1a15' },
  '.cm-activeLineGutter': { backgroundColor: '#1b1a15', color: accentGold },
})

export const signalDeckYaml = [theme, syntaxHighlighting(highlight)]
