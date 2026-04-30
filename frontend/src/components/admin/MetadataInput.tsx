import { useState, useRef, ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'

type Props = {
  metadataURL: string
  metadataXML: string
  onChange: (next: { metadataURL: string; metadataXML: string }) => void
  disabled?: boolean
}

export default function MetadataInput({ metadataURL, metadataXML, onChange, disabled }: Props) {
  const { t } = useTranslation()
  const initialMode: 'url' | 'xml' = metadataXML ? 'xml' : 'url'
  const [mode, setMode] = useState<'url' | 'xml'>(initialMode)
  const [editingXML, setEditingXML] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const switchMode = (next: 'url' | 'xml') => {
    setMode(next)
    if (next === 'url') {
      onChange({ metadataURL, metadataXML: '' })
    } else {
      onChange({ metadataURL: '', metadataXML })
    }
  }

  const handleFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const text = await file.text()
    onChange({ metadataURL: '', metadataXML: text })
    setEditingXML(false)
  }

  const handleClear = () => {
    onChange({ metadataURL: '', metadataXML: '' })
    if (fileRef.current) fileRef.current.value = ''
    setEditingXML(false)
  }

  return (
    <div className="metadata-input">
      <fieldset className="metadata-input-toggle">
        <legend>{t('admin.sso.field.metadata.toggle')}</legend>
        <label>
          <input
            type="radio"
            name="metadata-mode"
            value="url"
            checked={mode === 'url'}
            onChange={() => switchMode('url')}
            disabled={disabled}
          />
          {t('admin.sso.field.metadata.url')}
        </label>
        <label>
          <input
            type="radio"
            name="metadata-mode"
            value="xml"
            checked={mode === 'xml'}
            onChange={() => switchMode('xml')}
            disabled={disabled}
          />
          {t('admin.sso.field.metadata.xml')}
        </label>
      </fieldset>

      {mode === 'url' && (
        <input
          type="url"
          className="field-input"
          aria-label={t('admin.sso.field.metadata.url')}
          placeholder="https://idp.example.com/saml/metadata"
          value={metadataURL}
          onChange={(e) => onChange({ metadataURL: e.target.value, metadataXML: '' })}
          disabled={disabled}
        />
      )}

      {mode === 'xml' && (
        <div className="metadata-xml">
          <input
            ref={fileRef}
            type="file"
            accept=".xml,application/samlmetadata+xml,text/xml"
            onChange={handleFile}
            disabled={disabled}
            aria-label={t('admin.sso.field.metadata.upload')}
          />
          {metadataXML && (
            <>
              <textarea
                className="field-input metadata-xml-preview"
                rows={8}
                value={metadataXML}
                readOnly={!editingXML}
                onChange={(e) =>
                  onChange({ metadataURL: '', metadataXML: e.target.value })
                }
                disabled={disabled}
              />
              <div className="metadata-xml-actions">
                {!editingXML && (
                  <button type="button" onClick={() => setEditingXML(true)} disabled={disabled}>
                    {t('admin.sso.field.metadata.edit')}
                  </button>
                )}
                <button type="button" onClick={handleClear} disabled={disabled}>
                  {t('admin.sso.field.metadata.clear')}
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
