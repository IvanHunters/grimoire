import { useState, useRef } from 'react'
import { X, Upload, CheckCircle, AlertCircle, Database } from 'lucide-react'
import apiClient from '../../api/client'

interface ImportResult {
  notes: number
  folders: number
  tasks: number
  files: number
}

interface ImportDBModalProps {
  visible: boolean
  onClose: () => void
  onImported: () => void
}

export default function ImportDBModal({ visible, onClose, onImported }: ImportDBModalProps) {
  const [file, setFile] = useState<File | null>(null)
  const [status, setStatus] = useState<'idle' | 'loading' | 'done' | 'error'>('idle')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const reset = () => {
    setFile(null)
    setStatus('idle')
    setResult(null)
    setError(null)
  }

  const handleClose = () => {
    reset()
    onClose()
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0] ?? null
    setFile(f)
    setStatus('idle')
    setResult(null)
    setError(null)
  }

  const handleImport = async () => {
    if (!file) return
    setStatus('loading')
    setError(null)
    try {
      const form = new FormData()
      form.append('file', file)
      const resp = await apiClient.post<ImportResult>('/import/db', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
        timeout: 300_000,
      })
      setResult(resp.data)
      setStatus('done')
      onImported()
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: string }; message?: string })?.response?.data || (e as { message?: string })?.message || 'Import failed'
      setError(typeof msg === 'string' ? msg : 'Import failed')
      setStatus('error')
    }
  }

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-[2000]"
      onClick={e => { if (e.target === e.currentTarget) handleClose() }}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-md mx-4 overflow-hidden"
        style={{ boxShadow: '0 0 0 1px rgba(6,182,212,0.08), 0 25px 50px rgba(0,0,0,0.6)' }}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.06]">
          <div className="flex items-center gap-2">
            <Database className="w-3.5 h-3.5 text-cyan-500" />
            <span className="text-[10px] font-mono font-semibold tracking-widest text-cyan-500 uppercase">
              Restore Database
            </span>
          </div>
          <button onClick={handleClose} className="p-1 text-slate-600 hover:text-slate-400 transition-colors rounded">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 py-4 space-y-4">
          {/* Warning */}
          <div className="flex items-start gap-2 px-3 py-2.5 bg-amber-500/5 border border-amber-500/15 rounded">
            <AlertCircle className="w-3.5 h-3.5 text-amber-500/70 flex-shrink-0 mt-0.5" />
            <p className="text-[11px] font-mono text-amber-500/70 leading-relaxed">
              Existing notes, folders, and tasks with the same ID will be overwritten. Upload files will be replaced if paths match.
            </p>
          </div>

          {/* File picker */}
          <div>
            <input
              ref={inputRef}
              type="file"
              accept=".zip"
              onChange={handleFileChange}
              className="hidden"
            />
            <button
              onClick={() => inputRef.current?.click()}
              disabled={status === 'loading'}
              className="w-full flex items-center justify-center gap-2 px-4 py-3 border border-dashed border-white/[0.12] rounded hover:border-cyan-500/30 hover:bg-white/[0.02] transition-colors text-sm font-mono text-slate-500 hover:text-slate-300 disabled:opacity-40"
            >
              <Upload className="w-4 h-4" />
              {file ? file.name : 'Choose backup .zip'}
            </button>
            {file && (
              <p className="mt-1.5 text-[10px] font-mono text-slate-700 text-center">
                {(file.size / 1024 / 1024).toFixed(1)} MB
              </p>
            )}
          </div>

          {/* Result */}
          {status === 'done' && result && (
            <div className="flex items-start gap-2 px-3 py-2.5 bg-green-500/5 border border-green-500/15 rounded">
              <CheckCircle className="w-3.5 h-3.5 text-green-500/70 flex-shrink-0 mt-0.5" />
              <div className="text-[11px] font-mono text-green-500/70 space-y-0.5">
                <div>Import complete:</div>
                <div className="text-green-400/80">
                  {result.notes} notes · {result.folders} folders · {result.tasks} tasks · {result.files} files
                </div>
              </div>
            </div>
          )}

          {status === 'error' && error && (
            <div className="flex items-start gap-2 px-3 py-2.5 bg-red-500/5 border border-red-500/15 rounded">
              <AlertCircle className="w-3.5 h-3.5 text-red-500/70 flex-shrink-0 mt-0.5" />
              <p className="text-[11px] font-mono text-red-400/80">{error}</p>
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-white/[0.06]">
          <button
            onClick={handleClose}
            className="px-4 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
          >
            {status === 'done' ? 'close' : 'cancel'}
          </button>
          {status !== 'done' && (
            <button
              onClick={handleImport}
              disabled={!file || status === 'loading'}
              className="px-4 py-1.5 text-xs font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 hover:border-cyan-500/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              {status === 'loading' ? 'importing...' : 'restore →'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
