import { useState, useEffect } from 'react'
import { X, Upload, File } from 'lucide-react'

interface UploadModalProps {
  visible: boolean
  file: File | null
  onClose: () => void
  onConfirm: (fileName: string) => void
}

/**
 * UploadModal - File upload with preview and name editing
 *
 * Features:
 * - Image preview for images
 * - File info for documents/PDFs
 * - Editable file name
 * - Keyboard shortcuts (Enter to confirm, Escape to cancel)
 */
function UploadModal({ visible, file, onClose, onConfirm }: UploadModalProps) {
  const [fileName, setFileName] = useState('')
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)

  useEffect(() => {
    if (file) {
      // Set initial file name (without extension)
      const nameWithoutExt = file.name.replace(/\.[^/.]+$/, '')
      setFileName(nameWithoutExt)

      // Create preview URL for images
      if (file.type.startsWith('image/')) {
        const url = URL.createObjectURL(file)
        setPreviewUrl(url)

        // Cleanup on unmount
        return () => URL.revokeObjectURL(url)
      } else {
        setPreviewUrl(null)
      }
    }
  }, [file])

  const handleConfirm = () => {
    if (fileName.trim()) {
      onConfirm(fileName.trim())
      onClose()
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleConfirm()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  if (!visible || !file) return null

  const isImage = file.type.startsWith('image/')
  const fileSizeKB = (file.size / 1024).toFixed(2)
  const fileSizeMB = (file.size / (1024 * 1024)).toFixed(2)
  const sizeText = file.size > 1024 * 1024 ? `${fileSizeMB} MB` : `${fileSizeKB} KB`

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 z-[2000] flex items-center justify-center"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200">
          <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
            <Upload className="w-5 h-5 text-purple-600" />
            Upload File
          </h2>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-100 rounded transition"
            type="button"
          >
            <X className="w-5 h-5 text-gray-600" />
          </button>
        </div>

        {/* Body */}
        <div className="p-4 space-y-4">
          {/* Preview */}
          {isImage && previewUrl ? (
            <div className="bg-gray-50 rounded-lg p-4 border border-gray-200">
              <img
                src={previewUrl}
                alt="Preview"
                className="max-w-full max-h-64 mx-auto rounded"
              />
            </div>
          ) : (
            <div className="bg-gray-50 rounded-lg p-8 border border-gray-200 flex flex-col items-center justify-center">
              <File className="w-16 h-16 text-gray-400 mb-2" />
              <p className="text-sm text-gray-600">{file.type || 'Unknown type'}</p>
            </div>
          )}

          {/* File info */}
          <div className="bg-blue-50 rounded-lg p-3 border border-blue-200">
            <div className="text-xs text-blue-900 space-y-1">
              <div className="flex items-center justify-between">
                <span className="font-medium">Original name:</span>
                <span className="font-mono">{file.name}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="font-medium">Size:</span>
                <span>{sizeText}</span>
              </div>
            </div>
          </div>

          {/* File name input */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              File Name (without extension)
            </label>
            <input
              type="text"
              value={fileName}
              onChange={(e) => setFileName(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="my-file"
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              autoFocus
            />
            <p className="mt-1 text-xs text-gray-500">
              Press Enter to confirm, Escape to cancel
            </p>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 p-4 border-t border-gray-200">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={!fileName.trim()}
            className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            Upload
          </button>
        </div>
      </div>
    </div>
  )
}

export default UploadModal
