package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/database"
)

// KSQLService exposes optional ksqlDB integration.
type KSQLService struct {
	Client *database.KSQLClient
}

// NewKSQLService builds the ksqlDB REST client from config.
func NewKSQLService(injector do.Injector) (*KSQLService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()

	return &KSQLService{Client: database.NewKSQLClient(cfg.KSQL.Endpoint, cfg.KSQL.Timeout)}, nil
}
