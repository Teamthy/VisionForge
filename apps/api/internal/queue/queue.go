// Package queue provides a Redis-backed reliable job queue with a processing
// set for visibility timeout (at-least-once delivery; workers must be idempotent).
//
// Layout in Redis:
//
//	<name>:queued        LIST       jobs ready to run (LPUSH / BRPOP)
//	<name>:processing    ZSET       jobs currently being worked on, scored by deadline (uninx-seconds)
//	<name>:dead          ZSET       permanently-failed jobs scored by failed-at
//	<name>:payload:<id>  STRING     JSON JobPayload for a given id, TTL 24h
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

// Queue abstracts the job queue so tests can substitute a fake.
type Queue interface {
	Enqueue(ctx context.Context, job vtypes.JobPayload, delay time.Duration) error
	Dequeue(ctx context.Context, visibilityTimeout time.Duration) (*vtypes.JobPayload, error)
	Ack(ctx context.Context, jobID string) error
	Fail(ctx context.Context, jobID string, permanent bool, errMsg string) error
	RequeueExpired(ctx context.Context) (int, error)
	Depth(ctx context.Context) (int64, error)
	Close() error
}

// RedisQueue is a Queue backed by Redis.
type RedisQueue struct {
	c    *redis.Client
	name string
	ttl  time.Duration // TTL for payload keys
}

// NewRedisQueue constructs a RedisQueue.
func NewRedisQueue(c *redis.Client, queueName string) *RedisQueue {
	return &RedisQueue{c: c, name: queueName, ttl: 24 * time.Hour}
}

// key helpers
func (q *RedisQueue) keyQueued() string           { return q.name + ":queued" }
func (q *RedisQueue) keyProcessing() string       { return q.name + ":processing" }
func (q *RedisQueue) keyDead() string             { return q.name + ":dead" }
func (q *RedisQueue) keyPayload(id string) string { return q.name + ":payload:" + id }

// Enqueue places a job on the queue. If delay > 0, the job is added to the
// processing set with a future deadline so it becomes visible later — this is
// used for retry backoff.
func (q *RedisQueue) Enqueue(ctx context.Context, job vtypes.JobPayload, delay time.Duration) error {
	if job.JobID == "" {
		return errors.New("job id required")
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	pipe := q.c.TxPipeline()
	pipe.Set(ctx, q.keyPayload(job.JobID), payload, q.ttl)
	if delay > 0 {
		// Score = unix seconds at which the job becomes visible.
		pipe.ZAdd(ctx, q.keyProcessing(), redis.Z{
			Score:  float64(time.Now().Add(delay).Unix()),
			Member: job.JobID,
		})
	} else {
		pipe.LPush(ctx, q.keyQueued(), job.JobID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// Dequeue blocks until a job is available or ctx is cancelled. The job is moved
// atomically into the processing set with a deadline of now+visibilityTimeout.
func (q *RedisQueue) Dequeue(ctx context.Context, visibilityTimeout time.Duration) (*vtypes.JobPayload, error) {
	// First, move any expired delayed jobs back into the queued list.
	if _, err := q.RequeueExpired(ctx); err != nil {
		return nil, err
	}
	// BRPOPLPUSH is deprecated in favor of BLMOVE but we keep compatibility simple.
	// Use RPOPLPUSH semantically: pop from the RIGHT side and we can re-process.
	// Actually BRPOP from queued and add to processing in a Lua script for atomicity.
	script := redis.NewScript(`
local key_q = KEYS[1]
local key_p = KEYS[2]
local key_pay = KEYS[3]
local ttl = ARGV[2]
local id = redis.call("RPOP", key_q)
if not id then return nil end
local deadline = tonumber(ARGV[1])
redis.call("ZADD", key_p, deadline, id)
local payload = redis.call("GET", key_pay .. ":" .. id)
return {id, payload}
`)
	deadline := float64(time.Now().Add(visibilityTimeout).Unix())

	// Block using BLPOP-like wait: we poll with timeout to avoid missing.
	// In production a dedicated Lua-based BRPOP+ZADD would be ideal; we do a short BRPOP via raw.
	// For reliability we run a tight loop with short BRPOP.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Try atomic non-blocking dequeue first.
		res, err := script.Run(ctx, q.c, []string{q.keyQueued(), q.keyProcessing(), q.keyPayload("")[:len(q.keyPayload(""))]}, deadline, int(q.ttl.Seconds())).Result()
		// Our key_pay prefix approach is clumsy; instead we'll fetch payload after.
		if err == redis.Nil {
			// Block briefly.
			r, berr := q.c.BRPop(ctx, 2*time.Second, q.keyQueued()).Result()
			if berr != nil {
				if errors.Is(berr, redis.Nil) {
					continue
				}
				return nil, berr
			}
			if len(r) < 2 {
				continue
			}
			id := r[1]
			// Move into processing atomically via Lua.
			move := redis.NewScript(`
redis.call("ZADD", KEYS[1], ARGV[1], ARGV[2])
return 1
`)
			if _, err := move.Run(ctx, q.c, []string{q.keyProcessing()}, deadline, id).Result(); err != nil {
				// put back to queued to avoid losing the job
				_ = q.c.LPush(ctx, q.keyQueued(), id).Err()
				return nil, err
			}
			return q.fetchPayload(ctx, id)
		}
		if err != nil {
			return nil, fmt.Errorf("dequeue: %w", err)
		}
		// non-blocking path returned {id, payload}
		arr, ok := res.([]interface{})
		if !ok || len(arr) < 2 || arr[0] == nil {
			continue
		}
		id, _ := arr[0].(string)
		raw, _ := arr[1].(string)
		if raw == "" {
			// fetch payload
			return q.fetchPayload(ctx, id)
		}
		p, err := vtypes.UnmarshalJobPayload([]byte(raw))
		if err != nil {
			return nil, err
		}
		return &p, nil
	}
}

func (q *RedisQueue) fetchPayload(ctx context.Context, id string) (*vtypes.JobPayload, error) {
	raw, err := q.c.Get(ctx, q.keyPayload(id)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("fetch payload %s: %w", id, err)
	}
	p, err := vtypes.UnmarshalJobPayload(raw)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Ack confirms successful processing and removes the job from processing/payload.
func (q *RedisQueue) Ack(ctx context.Context, jobID string) error {
	pipe := q.c.TxPipeline()
	pipe.ZRem(ctx, q.keyProcessing(), jobID)
	pipe.Del(ctx, q.keyPayload(jobID))
	pipe.ZRem(ctx, q.keyDead(), jobID)
	_, err := pipe.Exec(ctx)
	return err
}

// Fail either requeues (transient) or moves to dead-letter set.
func (q *RedisQueue) Fail(ctx context.Context, jobID string, permanent bool, _ string) error {
	pipe := q.c.TxPipeline()
	pipe.ZRem(ctx, q.keyProcessing(), jobID)
	if permanent {
		pipe.ZAdd(ctx, q.keyDead(), redis.Z{Score: float64(time.Now().Unix()), Member: jobID})
	} else {
		// re-enqueue (caller sets delay)
		pipe.LPush(ctx, q.keyQueued(), jobID)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RequeueExpired scans the processing ZSET for jobs whose deadline has passed
// and moves them back to the queued list (worker crashed or lost lease).
func (q *RedisQueue) RequeueExpired(ctx context.Context) (int, error) {
	now := float64(time.Now().Unix())
	var requeued int
	for {
		// Fetch up to 100 expired jobs.
		jobs, err := q.c.ZRangeByScore(ctx, q.keyProcessing(), &redis.ZRangeBy{
			Min: "-inf", Max: strconv.FormatInt(time.Now().Unix(), 10), Offset: 0, Count: 100,
		}).Result()
		if err != nil {
			return requeued, err
		}
		if len(jobs) == 0 {
			break
		}
		// Atomically check-and-move using a Lua script to avoid races.
		script := redis.NewScript(`
local key_p = KEYS[1]
local key_q = KEYS[2]
local now = tonumber(ARGV[1])
local ids = ARGV[2]
local count = 0
-- ARGV[2] is actually one id per call for atomicity
local id = ARGV[2]
local score = redis.call("ZSCORE", key_p, id)
if score and tonumber(score) <= now then
    redis.call("ZREM", key_p, id)
    redis.call("LPUSH", key_q, id)
    count = 1
end
return count
`)
		for _, id := range jobs {
			n, err := script.Run(ctx, q.c, []string{q.keyProcessing(), q.keyQueued()}, now, id).Int()
			if err == nil {
				requeued += n
			}
		}
		if len(jobs) < 100 {
			break
		}
	}
	return requeued, nil
}

// Depth returns the current length of the queued list.
func (q *RedisQueue) Depth(ctx context.Context) (int64, error) {
	return q.c.LLen(ctx, q.keyQueued()).Result()
}

// Close terminates the underlying client connection.
func (q *RedisQueue) Close() error {
	return q.c.Close()
}
