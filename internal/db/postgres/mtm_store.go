package postgres

import memorypkg "flowweave/internal/domain/memory"

// MTMStore aliases the PostgreSQL-backed mid-term memory store.
type MTMStore = memorypkg.PgMTM

type MTMStoreConfig = memorypkg.PgMTMConfig

func NewMTMStore(cfg MTMStoreConfig) *MTMStore {
	return memorypkg.NewPgMTM(cfg)
}
