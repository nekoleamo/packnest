package store

// 台账/配置/锁的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeLedgerLoad     = "store.ledger_load"
	CodeLedgerCorrupt  = "store.ledger_corrupt"
	CodeNotInLedger    = "store.not_in_ledger"
	CodeLedgerEncode   = "store.ledger_encode"
	CodeConfigLoad     = "store.config_load"
	CodeConfigParse    = "store.config_parse"
	CodeConfigEncode   = "store.config_encode"
	CodeBackup         = "store.backup"
	CodeInstallRecord  = "store.install_record"
	CodeCacheIndexLoad = "store.cache_index_load"
	CodeCapacityEmpty  = "store.capacity_empty"
	CodeCapacityParse  = "store.capacity_parse"
	CodeLockMkdir      = "store.lock_mkdir"
	CodeLockOpen       = "store.lock_open"
	CodeLockPath       = "store.lock_path"
	CodeLockAcquire    = "store.lock_acquire"
)
