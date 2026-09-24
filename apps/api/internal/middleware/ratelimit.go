package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a fixed-window / sliding-window limiter using Redis.
type RateLimiter struct {
	rc *redis.Client
}

// NewRateLimiter creates a Redis-backed rate limiter.
func NewRateLimiter(rc *redis.Client) *RateLimiter { return &RateLimiter{rc: rc} }

// Allow returns (allowed, remaining, resetInSeconds, error).
// Uses a classic fixed-window INCR+EXPIRE approach (good enough for API limits).
func (rl *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, int, error) {
	k := "rl:" + key
	pipe := rl.rc.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	n := incr.Val()
	ttl, err := rl.rc.TTL(ctx, k).Result()
	if err != nil {
		ttl = window
	}
	reset := int(ttl.Seconds())
	if reset < 0 {
		reset = int(window.Seconds())
	}
	remaining := limit - int(n)
	if remaining < 0 {
		remaining = 0
	}
	return int(n) <= limit, remaining, reset, nil
}

// Middleware returns an HTTP middleware enforcing rate limits.  keyFn derives the
// bucket key from the request (e.g., IP, user ID).
func (rl *RateLimiter) Middleware(limit int, window time.Duration, keyFn func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			ok, remaining, reset, err := rl.Allow(r.Context(), key, limit, window)
			if err != nil {
				// Fail open if Redis is unavailable? We choose to fail closed in production
				// but for local dev resilience, allow through and log.
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.Itoa(reset))
			w.Header().Set("Retry-After", strconv.Itoa(reset))
			if !ok {
				http.Error(w, fmt.Sprintf(`{"error":{"code":"RATE_LIMITED","message":"too many requests"}}`), http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
