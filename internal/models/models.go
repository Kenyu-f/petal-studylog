// Package models defines the core domain types of StudyLog.
//
// These types are intentionally plain data structs (no behavior) so that
// the persistence layer (internal/store) can serialize them directly and
// the business logic (internal/grass, internal/handlers) can operate on
// them without hidden state. See explanation.md section 5 ("Data Model").
package models

import "time"

// User is one of the (at most a handful of) people using the app.
// MVP auth is username + password, see internal/authutil.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	PasswordHash string    `json:"passwordHash"`
	Salt         string    `json:"salt"`
	GroupID      string    `json:"groupId"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Group is a "Study Group" shared by two (or more) users. All Goals and
// Study Sessions are scoped to a Group so that partners see each other's
// logs. See explanation.md section 6 ("User / Group Model").
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// GoalType is a free-form label describing where in the hierarchy a Goal
// conceptually sits. It is NOT used to enforce a fixed 3-level structure;
// the actual structure comes from ParentGoalID and can be arbitrarily deep.
type GoalType string

const (
	GoalTypeLongTerm   GoalType = "long-term"
	GoalTypeMediumTerm GoalType = "medium-term"
	GoalTypeShortTerm  GoalType = "short-term"
)

// ProgressMode controls whether/how a Goal's progress bar is computed.
// See explanation.md section 11 ("Goal Progress Calculation") for the
// reasoning behind this three-way split.
type ProgressMode string

const (
	// ProgressNone: no numeric progress is shown. Used for goals whose
	// success criteria are not a simple sum (e.g. "Get into MIT").
	ProgressNone ProgressMode = "none"
	// ProgressCumulative: progress = (sum of linked minutes, all time,
	// including descendant goals) / TargetValue. Used for goals like
	// "Finish 3000 practice problems".
	ProgressCumulative ProgressMode = "cumulative"
	// ProgressRate: a recurring per-day target (e.g. "60 min/day") that
	// drives the Grass view rather than a lifetime progress bar.
	ProgressRate ProgressMode = "rate"
)

// Goal is a node in the goal tree. ParentGoalID == "" marks a root goal.
// Ownership is either a single user (OwnerID set) or the whole group
// (Shared == true, OwnerID empty) — see section 6.
type Goal struct {
	ID           string       `json:"id"`
	GroupID      string       `json:"groupId"`
	OwnerID      string       `json:"ownerId"` // "" if Shared
	Shared       bool         `json:"shared"`
	ParentGoalID string       `json:"parentGoalId"` // "" for root goals
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Type         GoalType     `json:"type"`
	ProgressMode ProgressMode `json:"progressMode"`
	// TargetValue/TargetUnit/TargetPeriod together describe the target.
	// MVP only gives numeric meaning to minutes (TargetUnit == "min"),
	// see explanation.md section 5 Decision D2.
	TargetValue  float64    `json:"targetValue"`  // 0 means "no numeric target"
	TargetUnit   string     `json:"targetUnit"`   // e.g. "min"
	TargetPeriod string     `json:"targetPeriod"` // "day" | "week" | "total"
	Deadline     *time.Time `json:"deadline,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// StudySession is a single logged block of study time.
type StudySession struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	GroupID     string    `json:"groupId"`
	Date        string    `json:"date"` // "YYYY-MM-DD", see explanation.md section 12
	DurationMin float64   `json:"durationMin"`
	Subject     string    `json:"subject"`
	GoalID      string    `json:"goalId"` // "" allowed: unlinked session
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
