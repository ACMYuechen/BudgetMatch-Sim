package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Address      string        `json:"address"`
	Password     string        `json:"password"`
	DB           int           `json:"db"`
	PoolSize     int           `json:"poolSize"`
	MinIdleConns int           `json:"minIdleConns"`
	DialTimeout  time.Duration `json:"dialTimeout"`
	ReadTimeout  time.Duration `json:"readTimeout"`
	WriteTimeout time.Duration `json:"writeTimeout"`
	PoolTimeout  time.Duration `json:"poolTimeout"`
	MaxRetries   int           `json:"maxRetries"`
}

type Redis struct {
	single *redis.Client
	// cluster *redis.ClusterClient
	// clusterMode bool
}

func NewRedisDB(c Config) (*Redis, error) {
	r := &Redis{}

	r.single = redis.NewClient(&redis.Options{
		Addr:         c.Address,
		Password:     c.Password,
		DB:           c.DB,
		PoolSize:     c.PoolSize,     // 连接池大小
		MinIdleConns: c.MinIdleConns, // 最小空闲连接数
		DialTimeout:  c.DialTimeout,  // 连接超时
		ReadTimeout:  c.ReadTimeout,  // 读超时
		WriteTimeout: c.WriteTimeout, // 写超时
		PoolTimeout:  c.PoolTimeout,  // 连接池获取连接的超时
		MaxRetries:   c.MaxRetries,   // 最大重试次数
	})

	if err := r.single.Ping(context.Background()).Err(); err != nil {
		return nil, errors.New("failed to connect to Redis: " + err.Error())
	}
	return r, nil
}

func (r *Redis) Close() error {
	return r.single.Close()
}

func (r *Redis) Client() redis.UniversalClient {
	return r.single
}

// Setex 设置带有过期时间的键值对
func (r *Redis) Setex(key string, value interface{}, expiration time.Duration) error {
	return r.single.Set(context.Background(), key, value, expiration).Err()
}

// Get 获取键值
func (r *Redis) Get(key string) (string, error) {
	return r.single.Get(context.Background(), key).Result()
}

// Del 删除键
func (r *Redis) Del(keys ...string) error {
	return r.single.Del(context.Background(), keys...).Err()
}
