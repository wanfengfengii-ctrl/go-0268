package domain

// TaskState is the lifecycle state of a leaf intake task. The twelve states
// form the aggregate state machine; only transitions declared in the
// transition table are legal.
type TaskState string

const (
	StatePendingLock    TaskState = "pending_lock"    // 待锁定
	StatePendingReceipt TaskState = "pending_receipt" // 待收青确认
	StateSlotOccupied   TaskState = "slot_occupied"   // 槽位占用中
	StateWithering      TaskState = "withering"       // 摊青采集中
	StateTenderness     TaskState = "tenderness"      // 嫩度核验中
	StateAssayScreening TaskState = "assay_screening" // 农残快筛中
	StateRetesting      TaskState = "retesting"       // 理化复测中
	StatePendingReview  TaskState = "pending_review"  // 待独立复核
	StateFixationReady  TaskState = "fixation_ready"  // 可杀青
	StateFixed          TaskState = "fixed"           // 已杀青
	StateRiskIsolated   TaskState = "risk_isolated"   // 风险隔离
	StateCancelled      TaskState = "cancelled"       // 已取消
)

// transitions declares every legal single-step transition in the aggregate
// state machine. A missing pair is illegal by definition.
var transitions = map[TaskState]map[TaskState]struct{}{
	StatePendingLock: {
		StatePendingReceipt: {},
		StateCancelled:      {},
	},
	StatePendingReceipt: {
		StateSlotOccupied: {},
		StateCancelled:    {},
	},
	StateSlotOccupied: {
		StateWithering: {},
		StateCancelled: {},
	},
	StateWithering: {
		StateTenderness: {},
		StateCancelled:  {},
	},
	StateTenderness: {
		StateAssayScreening: {},
		StateCancelled:      {},
	},
	StateAssayScreening: {
		StateRetesting: {},
		StateCancelled: {},
	},
	StateRetesting: {
		StatePendingReview: {},
		StateCancelled:     {},
	},
	StatePendingReview: {
		StateFixationReady: {},
		StateRiskIsolated:  {},
		StateCancelled:     {},
	},
	StateFixationReady: {
		StateFixed:        {},
		StateRiskIsolated: {},
		StateCancelled:    {},
	},
	StateFixed:        {},
	StateRiskIsolated: {},
	StateCancelled:    {},
}

// CanTransition reports whether the given transition is declared as legal.
func CanTransition(from, to TaskState) bool {
	if from == to {
		return false
	}
	targets, ok := transitions[from]
	if !ok {
		return false
	}
	_, ok = targets[to]
	return ok
}

// IsTerminal reports whether a state admits no further business operations.
// Risk isolation and cancellation are terminal; fixation is the completed
// terminal state.
func IsTerminal(s TaskState) bool {
	switch s {
	case StateFixed, StateRiskIsolated, StateCancelled:
		return true
	default:
		return false
	}
}

// IsCompleted reports whether the task reached the fixation-completed state.
func IsCompleted(s TaskState) bool {
	return s == StateFixed
}

// ValidStates returns the full set of recognized states, used by validation
// and documentation tests.
func ValidStates() []TaskState {
	return []TaskState{
		StatePendingLock,
		StatePendingReceipt,
		StateSlotOccupied,
		StateWithering,
		StateTenderness,
		StateAssayScreening,
		StateRetesting,
		StatePendingReview,
		StateFixationReady,
		StateFixed,
		StateRiskIsolated,
		StateCancelled,
	}
}
