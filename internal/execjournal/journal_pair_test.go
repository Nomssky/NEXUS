package execjournal

import (
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func TestGetPairReadsBothSidesAndPreservesAbsence(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("pair-left", "biz-1", "div-a", "actor"); err != nil {
		t.Fatal(err)
	}
	if err := j.Accepted("pair-right", "biz-1", "", "actor"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("pair-right", StateCompleted, ""); err != nil {
		t.Fatal(err)
	}

	left, right, leftOK, rightOK, err := j.GetPair("pair-left", "pair-right")
	if err != nil {
		t.Fatal(err)
	}
	if !leftOK || !rightOK {
		t.Fatalf("expected both records, got %v/%v", leftOK, rightOK)
	}
	if left.ID != "pair-left" || left.State != StateAccepted || left.DivisionID != "div-a" {
		t.Fatalf("left record wrong: %+v", left)
	}
	if right.ID != "pair-right" || right.State != StateCompleted {
		t.Fatalf("right record wrong: %+v", right)
	}

	_, _, leftOK, rightOK, err = j.GetPair("pair-left", "pair-missing")
	if err != nil {
		t.Fatal(err)
	}
	if !leftOK || rightOK {
		t.Fatalf("absence flags wrong: %v/%v", leftOK, rightOK)
	}

	if _, _, _, _, err := j.GetPair("", "pair-right"); err == nil {
		t.Fatal("expected empty-id rejection")
	}
	if _, _, _, _, err := j.GetPair("pair-left", "pair-left"); err == nil {
		t.Fatal("expected same-id rejection")
	}
}

func TestGetPairConcurrentWithTerminalStaysPerRecordCoherent(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("pair-left", "biz-1", "", "actor"); err != nil {
		t.Fatal(err)
	}
	if err := j.Accepted("pair-right", "biz-1", "", "actor"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				left, right, leftOK, rightOK, err := j.GetPair("pair-left", "pair-right")
				if err != nil {
					errs <- err
					return
				}
				if !leftOK || !rightOK {
					errs <- errPairMissing
					return
				}
				if left.ResearchEvidence != nil && !left.State.Terminal() {
					errs <- errPairMixed
					return
				}
				if right.ResearchEvidence != nil && !right.State.Terminal() {
					errs <- errPairMixed
					return
				}
				if err := ValidateResearchEvidenceForExecution(left.ID, left.ResearchEvidence); err != nil {
					errs <- err
					return
				}
				if err := ValidateResearchEvidenceForExecution(right.ID, right.ResearchEvidence); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := j.Terminal("pair-left", StateCompleted, ""); err != nil {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := j.Terminal("pair-right", StateFailed, "POLICY_DENIED"); err != nil {
			errs <- err
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

var (
	errPairMissing = pairTestError("pair record missing")
	errPairMixed   = pairTestError("pair record mixed a nonterminal state with evidence")
)

type pairTestError string

func (e pairTestError) Error() string { return string(e) }
