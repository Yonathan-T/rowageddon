package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	MaxConns int32
}

func Connect(ctx context.Context, connString string, cfg Config) (*pgxpool.Pool, error) {
	parsedConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	if cfg.MaxConns > 0 {
		parsedConfig.MaxConns = cfg.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, parsedConfig)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
