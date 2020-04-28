package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/qlcchain/go-qlc/common/util"
	"github.com/qlcchain/go-qlc/config"
)

func openPostgres(cfg *config.Config) (*sqlx.DB, error) {
	if err := util.CreateDirIfNotExist(cfg.SqliteDir()); err != nil {
		return nil, err
	}

	db, err := sqlx.Connect(cfg.DB.Driver, cfg.DB.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("connect postgres error: %s", err)
	}
	//DBStore.SetMaxOpenConns(200)
	//DBStore.SetMaxIdleConns(100)

	return db, nil
}
