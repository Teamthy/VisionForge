# ADR-001: Redis-backed reliable job queue

## Context
We need async job processing with at-least-once delivery semantics for CV inference.

## Decision
Implement a lightweight Redis queue (`LIST` for queued, `ZSET` for in-flight with
deadline scores, `ZSET` for dead letters). Workers `BRPOP` jobs into the processing
set with a visibility timeout; expired entries are reclaimed. Status transitions
are validated in the database with a CAS-like `UPDATE ... WHERE status=?`.

## Consequences
- Pro: no new infrastructure (Redis is already there for rate limits/caching).
- Pro: works great for a single region and moderate throughput.
- Con: not partition-tolerant; Kafka/NATS is the natural upgrade when we need
  cross-region streams or replay. The queue interface (`queue.Queue`) is kept
  narrow so the implementation can be swapped.

## Status
Accepted.
