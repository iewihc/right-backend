package infra

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type Redis struct {
	Client *redis.Client
}

func NewRedis(config RedisConfig) (*Redis, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Println("Connected to Redis!")

	return &Redis{
		Client: rdb,
	}, nil
}

func (r *Redis) Close() error {
	return r.Client.Close()
}
