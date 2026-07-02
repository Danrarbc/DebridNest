package admin

const (
	ActionAddMagnet        = "torrent.add_magnet"
	ActionUploadTorrent    = "torrent.upload"
	ActionDeleteTorrent    = "torrent.delete"
	ActionBatchDelete      = "torrent.batch_delete"
	ActionRetryTorrent     = "torrent.retry"
	ActionPurgeTorrents    = "torrent.purge"
	ActionLibraryPatch     = "library.patch"
	ActionMaintenance      = "maintenance.cleanup"
	ActionSettingsPatch    = "settings.patch"
	ActionStorageReconcile = "storage.reconcile"
	ActionStoragePolicy    = "storage.policy"
	ActionImportCreate     = "import.rss.create"
	ActionImportRun        = "import.rss.run"
	ActionImportDelete     = "import.rss.delete"
	ActionUserCreate       = "user.create"
	ActionUserUpdate       = "user.update"
	ActionUserDelete       = "user.delete"
	ActionUserRotateToken  = "user.rotate_token"
	ActionShareCreate      = "share.create"
	ActionShareRevoke      = "share.revoke"
)
