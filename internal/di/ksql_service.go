package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/ksql"
)

// KSQLService exposes optional ksqlDB integration.
type KSQLService struct {
	Client *ksql.Client
}

// NewKSQLService builds the ksqlDB REST client from config.
func NewKSQLService(injector do.Injector) (*KSQLService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()

	return &KSQLService{Client: ksql.NewClient(cfg.KSQL.Endpoint, cfg.KSQL.Timeout)}, nil
}
