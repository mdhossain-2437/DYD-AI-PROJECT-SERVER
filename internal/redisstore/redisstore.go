// Package redisstore adapts a go-redis client to Fiber's Storage interface so
// the rate limiter (and any other Fiber middleware needing shared state) uses
// Redis. This is what makes rate limits hold ACROSS every API instance behind
// the load balancer instead of per-process — essential for real DDoS/abuse
// mitigation when horizontally scaled.
package redisstore

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Storage struct {
	client *redis.Client
	prefix string
}

// Connect parses a redis URL, verifies connectivity, and returns a Storage.
// A nil Storage (with nil error) is returned when url is empty — callers then
// fall back to in-memory limiting.
func Connect(url string, log zerolog.Logger) (*Storage, error) {
	if url == "" {
		return nil, nil
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	log.Info().Msg("redis connected (distributed rate limiting enabled)")
	return &Storage{client: client, prefix: "dyd:"}, nil
}

func (s *Storage) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, nil
	}
	val, err := s.client.Get(context.Background(), s.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return val, err
}

func (s *Storage) Set(key string, val []byte, exp time.Duration) error {
	if key == "" || len(val) == 0 {
		return nil
	}
	return s.client.Set(context.Background(), s.prefix+key, val, exp).Err()
}

func (s *Storage) Delete(key string) error {
	if key == "" {
		return nil
	}
	return s.client.Del(context.Background(), s.prefix+key).Err()
}

func (s *Storage) Reset() error {
	return s.client.FlushDB(context.Background()).Err()
}

func (s *Storage) Close() error {
	return s.client.Close()
}

// Ping reports Redis health for the readiness probe.
func (s *Storage) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
