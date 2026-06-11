package database

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Config struct {
	LogLevel        int           `json:"logLevel"`
	Driver          string        `json:"driver"`
	DSN             string        `json:"dsn"`
	MaxOpenConns    int           `json:"maxOpenConns"`
	MaxIdleConns    int           `json:"maxIdleConns"`
	ConnMaxLifetime time.Duration `json:"connMaxLifetime"`
	ConnMaxIdleTime time.Duration `json:"connMaxIdleTime"`
	AutoMigrate     bool          `json:"autoMigrate"`
	PrepareStmt     bool          `json:"prepareStmt"`
}

type Database struct {
	db *gorm.DB
}

func NewDatabase(cfg Config) (*Database, error) {
	gormcfg := &gorm.Config{
		PrepareStmt: cfg.PrepareStmt,
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN), gormcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB from gorm.DB: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	// 测试数据库连接
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Database{db: db}, nil
}

func (d *Database) DB() *gorm.DB {
	return d.db
}

func (d *Database) Close() {
	sqlDB, err := d.db.DB()
	if err != nil {
		panic(err)
	}
	if err := sqlDB.Close(); err != nil {
		panic(err)
	}
}
