import { useEffect } from 'react'
import { useStore } from '../store'

// Map user preference to existing [data-theme="..."] values in global.css.
// signal = dark (Signal Deck accent cyan), paper = light (rust on cream).
const THEMES = { dark: 'signal', light: 'paper' } as const

export function useTheme() {
  const pref = useStore((s) => s.me?.preferences.theme ?? 'system')

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')

    const apply = () => {
      const effective = pref === 'system' ? (mq.matches ? 'dark' : 'light') : pref
      document.documentElement.setAttribute('data-theme', THEMES[effective as 'dark' | 'light'])
    }

    apply()
    if (pref !== 'system') return
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [pref])
}
