package modelrouter

import (
	"sync"
)

// InvocationAccounting tracks model invocations for cost and token accounting.
type InvocationAccounting struct {
	records     []*InvocationRecord
	totalTokens int64
	totalCost   float64
	mu          sync.RWMutex
}

// NewInvocationAccounting creates a new invocation accounting.
func NewInvocationAccounting() *InvocationAccounting {
	return &InvocationAccounting{}
}

// Record adds an invocation record.
func (ia *InvocationAccounting) Record(record *InvocationRecord) {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	ia.records = append(ia.records, record)
	ia.totalTokens += int64(record.TotalTokens)
	ia.totalCost += record.Cost
}

// TotalTokens returns the total tokens used across all invocations.
func (ia *InvocationAccounting) TotalTokens() int64 {
	ia.mu.RLock()
	defer ia.mu.RUnlock()
	return ia.totalTokens
}

// TotalCost returns the total cost across all invocations.
func (ia *InvocationAccounting) TotalCost() float64 {
	ia.mu.RLock()
	defer ia.mu.RUnlock()
	return ia.totalCost
}

// Records returns all invocation records.
func (ia *InvocationAccounting) Records() []*InvocationRecord {
	ia.mu.RLock()
	defer ia.mu.RUnlock()
	result := make([]*InvocationRecord, len(ia.records))
	copy(result, ia.records)
	return result
}

// RecordCount returns the total number of invocations.
func (ia *InvocationAccounting) RecordCount() int {
	ia.mu.RLock()
	defer ia.mu.RUnlock()
	return len(ia.records)
}

// TokensByModel returns tokens used per model.
func (ia *InvocationAccounting) TokensByModel() map[string]int64 {
	ia.mu.RLock()
	defer ia.mu.RUnlock()

	result := make(map[string]int64)
	for _, r := range ia.records {
		if r.Success {
			result[r.ModelID] += int64(r.TotalTokens)
		}
	}
	return result
}

// CostByModel returns cost per model.
func (ia *InvocationAccounting) CostByModel() map[string]float64 {
	ia.mu.RLock()
	defer ia.mu.RUnlock()

	result := make(map[string]float64)
	for _, r := range ia.records {
		if r.Success {
			result[r.ModelID] += r.Cost
		}
	}
	return result
}

// TokensByBusiness returns tokens used per business.
func (ia *InvocationAccounting) TokensByBusiness() map[string]int64 {
	ia.mu.RLock()
	defer ia.mu.RUnlock()

	result := make(map[string]int64)
	for _, r := range ia.records {
		if r.Success {
			result[r.BusinessID] += int64(r.TotalTokens)
		}
	}
	return result
}
