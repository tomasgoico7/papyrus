package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucket is a token bucket that several replicas can share.
//
// It runs as one script because the read, the refill and the spend have to be a
// single step. Doing them as separate commands lets two replicas both read four
// tokens left, both spend one, and both write three — so a budget of twenty
// quietly becomes forty.
//
// The clock is Redis's own, not the callers'. Replicas do not agree on what time
// it is, and a bucket refilled against a fast clock hands out tokens that were
// never earned.
const tokenBucket = `
local key    = KEYS[1]
local rate   = tonumber(ARGV[1])
local burst  = tonumber(ARGV[2])
local ttl    = tonumber(ARGV[3])

local clock = redis.call('TIME')
local now = tonumber(clock[1]) + tonumber(clock[2]) / 1000000

local bucket = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(bucket[1])
local ts     = tonumber(bucket[2])

if tokens == nil or ts == nil then
  tokens = burst
  ts = now
end

local elapsed = now - ts
if elapsed < 0 then
  elapsed = 0
end
tokens = math.min(burst, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, ttl)

return allowed
`

// Shared limits a caller across every replica that talks to the same Redis.
type Shared struct {
	client  *redis.Client
	script  *redis.Script
	rate    float64
	burst   int
	ttl     time.Duration
	timeout time.Duration
	prefix  string
}

// NewShared builds the limiter from the same connection URL the cache uses.
//
// The per-call timeout is short on purpose: a limiter that makes every request
// wait on a slow Redis has become the outage it was meant to prevent.
func NewShared(url string, requestsPerMinute int, timeout time.Duration) (*Shared, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("ratelimit: parsing redis url: %w", err)
	}
	options.DialTimeout = timeout
	options.ReadTimeout = timeout
	options.WriteTimeout = timeout

	if requestsPerMinute < 1 {
		requestsPerMinute = 1
	}

	return &Shared{
		client: redis.NewClient(options),
		script: redis.NewScript(tokenBucket),
		rate:   float64(requestsPerMinute) / 60.0,
		burst:  requestsPerMinute,
		// Long enough that a bucket survives between bursts, short enough that
		// callers who never come back stop taking up room.
		ttl:     10 * time.Minute,
		timeout: timeout,
		prefix:  "ratelimit:",
	}, nil
}

func (s *Shared) Allow(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	allowed, err := s.script.Run(ctx, s.client,
		[]string{s.prefix + key},
		s.rate, s.burst, s.ttl.Milliseconds(),
	).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis: %w", err)
	}
	return allowed == 1, nil
}

// Ping reports whether the instance is reachable, for a line in the startup log.
func (s *Shared) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.client.Ping(ctx).Err()
}

func (s *Shared) Close() error {
	return s.client.Close()
}
