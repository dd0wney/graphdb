package faultsim

import "testing"

// faultsim is an instrument, so each test checks that it both fires when armed
// and stays silent when not. A fault injector that fires when it should not is
// worse than none: it makes every other test a liar.

func TestFail_SilentUntilArmed(t *testing.T) {
	Reset()
	defer Reset()

	for i := 0; i < 5; i++ {
		if Fail(WALLSNExhausted) {
			t.Fatalf("call %d fired with nothing armed", i)
		}
	}
	// Calls stays at zero here on purpose: with nothing armed, Fail returns on
	// an atomic load and records nothing. That is the production path, and
	// counting on it would cost every caller a lock for no benefit.
	if Calls(WALLSNExhausted) != 0 {
		t.Errorf("Calls = %d, want 0 while nothing is armed", Calls(WALLSNExhausted))
	}

	// Once a DIFFERENT site is armed, the counting path is live and an
	// unarmed site still records its calls. That is what makes Calls usable as
	// a negative control.
	Arm(WALRotateReopen, 0)
	for i := 0; i < 3; i++ {
		if Fail(WALLSNExhausted) {
			t.Fatalf("unarmed site fired on call %d", i)
		}
	}
	if got := Calls(WALLSNExhausted); got != 3 {
		t.Errorf("Calls = %d, want 3 once counting is live", got)
	}
}

func TestFail_ArmedFiresEveryCall(t *testing.T) {
	Reset()
	defer Reset()

	Arm(WALLSNExhausted, 0)
	for i := 0; i < 3; i++ {
		if !Fail(WALLSNExhausted) {
			t.Fatalf("call %d did not fire", i)
		}
	}
	if Fired(WALLSNExhausted) != 3 {
		t.Errorf("Fired = %d, want 3", Fired(WALLSNExhausted))
	}
}

// TestFail_NthOnly is the mode a sweep needs: walk the failure through a
// sequence of attempts, one at a time.
func TestFail_NthOnly(t *testing.T) {
	Reset()
	defer Reset()

	Arm(WALRotateReopen, 3)
	got := []bool{}
	for i := 0; i < 5; i++ {
		got = append(got, Fail(WALRotateReopen))
	}
	want := []bool{false, false, true, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call %d = %v, want %v (fired on the wrong call)", i+1, got[i], want[i])
		}
	}
}

func TestFail_SitesAreIndependent(t *testing.T) {
	Reset()
	defer Reset()

	Arm(WALLSNExhausted, 0)
	if Fail(WALRotateReopen) {
		t.Error("arming one site fired a different one")
	}
	if !Fail(WALLSNExhausted) {
		t.Error("the armed site did not fire")
	}
}

func TestDisarmAndReset(t *testing.T) {
	Reset()
	defer Reset()

	Arm(WALLSNExhausted, 0)
	Disarm(WALLSNExhausted)
	if Fail(WALLSNExhausted) {
		t.Error("a disarmed site fired")
	}

	Arm(WALLSNExhausted, 0)
	Arm(WALRotateReopen, 0)
	Reset()
	if Fail(WALLSNExhausted) || Fail(WALRotateReopen) {
		t.Error("a site fired after Reset")
	}
}

// TestFail_UnknownSiteNeverFires guards the fast path: an out-of-range Site
// must be inert rather than panic on the array index.
func TestFail_UnknownSiteNeverFires(t *testing.T) {
	Reset()
	defer Reset()

	Arm(Site(999), 0)
	if Fail(Site(999)) {
		t.Error("an out-of-range site fired")
	}
	if Fail(None) {
		t.Error("None fired")
	}
}
