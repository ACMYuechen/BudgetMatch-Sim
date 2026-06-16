package main

import (
	"os"
	"testing"

	"budgetmatch-sim/services/rpc/auth/internal/config"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestLoadConfig(t *testing.T) {
	_ = os.Setenv("ETCD_HOSTS", "127.0.0.1:2379")
	_ = os.Setenv("JWT_SECRET", "test-secret")

	var c config.Config
	if err := conf.Load("etc/config.yaml", &c, conf.UseEnv()); err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if c.Etcd.Key != "auth.rpc" {
		t.Errorf("etcd key mismatch, got %s", c.Etcd.Key)
	}
}
