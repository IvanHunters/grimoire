import { X, AlertTriangle } from 'lucide-react'

interface DeleteConfirmModalProps {
  visible: boolean
  onClose: () => void
  type: 'note' | 'folder'
  itemName: string
  onConfirm: () => void
}

function DeleteConfirmModal({ visible, onClose, type, itemName, onConfirm }: DeleteConfirmModalProps) {
  if (!visible) return null

  const handleConfirm = () => {
    onConfirm()
    onClose()
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-[2000]"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200">
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-red-600" />
            <h2 className="text-lg font-semibold text-gray-900">
              Delete {type === 'note' ? 'Note' : 'Folder'}
            </h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-100 rounded transition"
            type="button"
          >
            <X className="w-5 h-5 text-gray-600" />
          </button>
        </div>

        {/* Body */}
        <div className="p-4">
          <p className="text-gray-700">
            Are you sure you want to delete{' '}
            <span className="font-semibold">{itemName}</span>?
          </p>
          {type === 'folder' && (
            <p className="mt-2 text-sm text-red-600">
              ⚠️ All notes inside this folder will also be deleted!
            </p>
          )}
          <p className="mt-2 text-sm text-gray-500">
            This action cannot be undone.
          </p>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 p-4 border-t border-gray-200">
          <button
            onClick={onClose}
            className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition"
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            className="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 transition"
          >
            Delete {type === 'note' ? 'Note' : 'Folder'}
          </button>
        </div>
      </div>
    </div>
  )
}

export default DeleteConfirmModal
