import { useState, useEffect } from 'react'
import { X, Sparkles } from 'lucide-react'
import { skillsAPI } from '../../api/skills'

interface NewSkillModalProps {
  visible: boolean
  onClose: () => void
  onCreated: () => void
}

function NewSkillModal({ visible, onClose, onCreated }: NewSkillModalProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [content, setContent] = useState('')
  const [allowedTools, setAllowedTools] = useState('')
  const [disableModelInvocation, setDisableModelInvocation] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (visible) {
      setName('')
      setDescription('')
      setContent('')
      setAllowedTools('')
      setDisableModelInvocation(false)
      setError(null)
    }
  }, [visible])

  if (!visible) return null

  const valid = /^[a-z0-9-]{1,64}$/.test(name) && description.trim().length > 0 && content.trim().length > 0

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!valid || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const frontmatter: Record<string, unknown> = {}
      if (allowedTools.trim()) frontmatter['allowed-tools'] = allowedTools.trim()
      if (disableModelInvocation) frontmatter['disable-model-invocation'] = true
      await skillsAPI.create({ name, description, content, frontmatter })
      onCreated()
      onClose()
    } catch (err: any) {
      setError(err?.response?.data?.error || err?.message || 'Failed to create skill')
    } finally {
      setSubmitting(false)
    }
  }

  const inputCls = 'w-full px-3 py-2 bg-white/[0.04] border border-white/[0.09] rounded text-sm text-slate-200 font-mono placeholder-slate-600 focus:outline-none focus:border-amber-500/50 focus:bg-white/[0.06] transition-colors'
  const labelCls = 'block text-[10px] font-semibold tracking-widest text-slate-600 font-mono uppercase mb-1.5'

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-[2000]"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-2xl mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
        style={{ boxShadow: '0 0 0 1px rgba(245,158,11,0.08), 0 25px 50px rgba(0,0,0,0.6)' }}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.06]">
          <div className="flex items-center gap-2">
            <Sparkles className="w-3.5 h-3.5 text-amber-500" />
            <span className="text-[10px] font-mono font-semibold tracking-widest text-amber-500 uppercase">
              New Skill
            </span>
          </div>
          <button onClick={onClose} className="p-1 text-slate-600 hover:text-slate-400 transition-colors rounded" type="button">
            <X className="w-4 h-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="px-5 py-4 space-y-3">
            <div>
              <label className={labelCls}>Name (lowercase, digits, hyphens)</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-skill"
                className={inputCls}
                autoFocus
              />
            </div>
            <div>
              <label className={labelCls}>Description (when Claude should use this)</label>
              <input
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Summarize the uncommitted changes and flag risks."
                className={inputCls}
              />
            </div>
            <div>
              <label className={labelCls}>Allowed tools (optional, space-separated)</label>
              <input
                type="text"
                value={allowedTools}
                onChange={(e) => setAllowedTools(e.target.value)}
                placeholder="Read Grep Bash(git *)"
                className={inputCls}
              />
            </div>
            <div className="flex items-center gap-2">
              <input
                id="disable-model"
                type="checkbox"
                checked={disableModelInvocation}
                onChange={(e) => setDisableModelInvocation(e.target.checked)}
                className="rounded"
              />
              <label htmlFor="disable-model" className="text-[11px] font-mono text-slate-400">
                user-only (Claude cannot trigger automatically)
              </label>
            </div>
            <div>
              <label className={labelCls}>Body (markdown content of SKILL.md)</label>
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                placeholder={'## Instructions\n\nDo the thing...\n'}
                rows={10}
                className={inputCls + ' font-mono resize-y'}
              />
            </div>
            {error && (
              <p className="text-xs font-mono text-red-400">{error}</p>
            )}
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-white/[0.06]">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
            >
              cancel
            </button>
            <button
              type="submit"
              disabled={!valid || submitting}
              className="px-4 py-1.5 text-xs font-mono bg-amber-500/15 text-amber-400 border border-amber-500/25 rounded hover:bg-amber-500/25 hover:border-amber-500/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              {submitting ? 'creating...' : 'create →'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default NewSkillModal
