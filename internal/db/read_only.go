package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenReadOnly opens a database connection without running migrations or DDL.
// SQLite is opened with mode=ro and query_only; server databases use a
// read-only transaction so the preflight cannot modify rows accidentally.
func OpenReadOnly(dbType, dsn string) (*gorm.DB, func() error, error) {
	config := &gorm.Config{Logger: logger.Discard}
	var (
		connection *gorm.DB
		err        error
	)
	switch dbType {
	case "sqlite":
		uri := dsn
		if !strings.HasPrefix(uri, "file:") {
			uri = "file:" + strings.ReplaceAll(uri, "\\", "/")
		}
		separator := "?"
		if strings.Contains(uri, "?") {
			separator = "&"
		}
		connection, err = gorm.Open(sqlite.Open(uri+separator+"mode=ro&_pragma=query_only(ON)"), config)
	case "mysql":
		connection, err = gorm.Open(mysql.Open(dsn), config)
	case "postgres", "postgresql":
		connection, err = gorm.Open(postgres.Open(dsn), config)
	default:
		return nil, nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := connection.DB()
	if err != nil {
		return nil, nil, err
	}
	if dbType == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
		return connection, sqlDB.Close, nil
	}
	transaction := connection.Begin(&sql.TxOptions{ReadOnly: true})
	if transaction.Error != nil {
		_ = sqlDB.Close()
		return nil, nil, transaction.Error
	}
	closeFn := func() error {
		rollbackErr := transaction.Rollback().Error
		closeErr := sqlDB.Close()
		if rollbackErr != nil && !strings.Contains(strings.ToLower(rollbackErr.Error()), "already") {
			return rollbackErr
		}
		return closeErr
	}
	return transaction, closeFn, nil
}
