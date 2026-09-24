// Unit tests for the job state machine and error-code retriability.
// These are the core reliability invariants of the async pipeline:
// the worker refuses illegal transitions and only re-enqueues retriable errors.
package types

import (
	"testing"
)

func TestJobStatusValidTransition(t *testing.T) {
	cases := []struct {
		from, to JobStatus
		want     bool
	}{
		{JobStatusCreated, JobStatusQueued, true},
		{JobStatusCreated, JobStatusCancelled, true},
		{JobStatusCreated, JobStatusFailed, true},
		{JobStatusCreated, JobStatusRunning, false},
		{JobStatusCreated, JobStatusSuccess, false},
		{JobStatusQueued, JobStatusRunning, true},
		{JobStatusQueued, JobStatusRetrying, true},
		{JobStatusQueued, JobStatusDead, true},
		{JobStatusQueued, JobStatusSuccess, false},
		{JobStatusRunning, JobStatusSuccess, true},
		{JobStatusRunning, JobStatusFailed, true},
		{JobStatusRunning, JobStatusRetrying, true},
		{JobStatusRunning, JobStatusQueued, false},
		{JobStatusFailed, JobStatusRetrying, true},
		{JobStatusFailed, JobStatusDead, true},
		{JobStatusFailed, JobStatusSuccess, false},
		{JobStatusRetrying, JobStatusQueued, true},
		{JobStatusRetrying, JobStatusDead, true},
		{JobStatusSuccess, JobStatusRunning, false},
		{JobStatusSuccess, JobStatusFailed, false},
		{JobStatusDead, JobStatusQueued, false},
		{JobStatusCancelled, JobStatusRunning, false},
	}
	for _, tc := range cases {
		if got := tc.from.ValidTransition(tc.to); got != tc.want {
			t.Errorf("transition %s->%s = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestTerminalStatesAreFinal(t *testing.T) {
	terminal := []JobStatus{JobStatusSuccess, JobStatusDead, JobStatusCancelled}
	all := []JobStatus{
		JobStatusCreated, JobStatusQueued, JobStatusRunning,
		JobStatusSuccess, JobStatusFailed, JobStatusRetrying,
		JobStatusDead, JobStatusCancelled,
	}
	for _, from := range terminal {
		for _, to := range all {
			if from.ValidTransition(to) {
				t.Errorf("terminal state %s must not transition to %s", from, to)
			}
		}
	}
}

func TestErrorCodeRetriable(t *testing.T) {
	cases := map[ErrorCode]bool{
		ErrCodeTimeout:          true,
		ErrCodeQueue:            true,
		ErrCodeDatabase:         true,
		ErrCodeStorage:          true,
		ErrCodeMLService:        true,
		ErrCodeInternal:         false,
		ErrCodeInvalidInput:     false,
		ErrCodeNotFound:         false,
		ErrCodeUnauthorized:     false,
		ErrCodeUnsupportedModel: false,
		ErrCodeCorruptFile:      false,
		ErrCodeRateLimited:      false,
	}
	for code, want := range cases {
		if got := code.Retriable(); got != want {
			t.Errorf("Retriable(%s) = %v, want %v", code, got, want)
		}
	}
}
