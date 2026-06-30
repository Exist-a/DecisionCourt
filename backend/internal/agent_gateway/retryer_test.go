package agent_gateway

import (
	"errors"
	"testing"
	"time"
)

func TestRetryer_SuccessNoRetry(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})
	calls := 0
	err := r.Do(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls: want 1 got %d", calls)
	}
	if r.LastCount() != 0 {
		t.Errorf("retry count: want 0 got %d", r.LastCount())
	}
}

func TestRetryer_RetryOnceThenSuccess(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})
	calls := 0
	err := r.Do(func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls: want 2 got %d", calls)
	}
	if r.LastCount() != 1 {
		t.Errorf("retry count: want 1 got %d", r.LastCount())
	}
}

func TestRetryer_FailsAfterMaxRetries(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})
	calls := 0
	err := r.Do(func() error {
		calls++
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 4 { // initial + 3 retries
		t.Errorf("calls: want 4 got %d", calls)
	}
	if r.LastCount() != 3 {
		t.Errorf("retry count: want 3 got %d", r.LastCount())
	}
}

func TestRetryer_NoRetryWhenDisabled(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{})
	calls := 0
	err := r.Do(func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 1 {
		t.Errorf("calls: want 1 got %d", calls)
	}
	if r.LastCount() != 0 {
		t.Errorf("retry count: want 0 got %d", r.LastCount())
	}
}
