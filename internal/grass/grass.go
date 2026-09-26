// Package grass computes the Grass heatmap and Goal progress numbers.
//
// This package is deliberately pure (no store/HTTP dependencies): it takes
// already-loaded Goals/Sessions and returns numbers. That keeps the "what
// does 75% mean" logic in one small, testable place. See explanation.md
// sections 9 ("Grass Calculation"), 10 ("Grass Color Normalization") and
// 11 ("Goal Progress Calculation") for the full rationale.
package grass

import (
	"sort"

	"studylog/internal/models"
)

// Cell is one day in the Grass calendar for a specific Goal.
type Cell struct {
	Date      string  `json:"date"`      // "YYYY-MM-DD"
	ActualMin float64 `json:"actualMin"` // sum of durations that day
	TargetMin float64 `json:"targetMin"` // the goal's daily target
	Ratio     float64 `json:"ratio"`     // actual / target, NOT capped
	Intensity float64 `json:"intensity"` // min(ratio, 1) — drives color
}

// BuildCells computes one Cell per date in [start, end] (inclusive,
// "YYYY-MM-DD" strings) for the given goal, from the given sessions.
// Sessions are expected to already be filtered to this goal's ID.
//
// Formula (explanation.md section 9):
//
//	actual(date) = sum(duration of sessions on date whose GoalID == goal.ID)
//	ratio(date)  = actual(date) / goal.TargetValue      (0 if target == 0)
//	intensity    = min(ratio, 1)                        (drives cell color)
func BuildCells(goal models.Goal, sessions []models.StudySession, dates []string) []Cell {
	sums := map[string]float64{}
	for _, sess := range sessions {
		if sess.GoalID != goal.ID {
			continue
		}
		sums[sess.Date] += sess.DurationMin
	}

	cells := make([]Cell, 0, len(dates))
	for _, d := range dates {
		actual := sums[d]
		var ratio float64
		if goal.TargetValue > 0 {
			ratio = actual / goal.TargetValue
		}
		intensity := ratio
		if intensity > 1 {
			intensity = 1
		}
		if intensity < 0 {
			intensity = 0
		}
		cells = append(cells, Cell{
			Date:      d,
			ActualMin: actual,
			TargetMin: goal.TargetValue,
			Ratio:     ratio,
			Intensity: intensity,
		})
	}
	return cells
}

// Progress describes a Goal's progress bar, or the absence of one.
type Progress struct {
	Mode       models.ProgressMode `json:"mode"`
	ActualMin  float64             `json:"actualMin"`
	TargetMin  float64             `json:"targetMin"`
	Percent    float64             `json:"percent"`    // 0-100+, only meaningful if Mode == cumulative
	HasPercent bool                `json:"hasPercent"` // false => UI must not render a bar/number
}

// ComputeProgress computes a Goal's progress. Only ProgressCumulative goals
// get a percentage; ProgressNone and ProgressRate goals get HasPercent =
// false (rate-goals are represented by Grass, not a lifetime bar; see
// explanation.md section 11 for why we refuse to invent a number for
// ProgressNone goals like "Get into MIT").
//
// For cumulative goals, ActualMin aggregates recursively: this goal's own
// directly-linked sessions PLUS every descendant goal's actual (spec
// section 7: "親Goalの進捗については、その配下のGoalやStudy Sessionから
// 集計できる構造にしてください").
func ComputeProgress(goal models.Goal, allGoals []models.Goal, allSessions []models.StudySession) Progress {
	p := Progress{Mode: goal.ProgressMode, TargetMin: goal.TargetValue}
	if goal.ProgressMode != models.ProgressCumulative || goal.TargetValue <= 0 {
		return p
	}
	childrenByParent := map[string][]models.Goal{}
	for _, g := range allGoals {
		childrenByParent[g.ParentGoalID] = append(childrenByParent[g.ParentGoalID], g)
	}
	sessionsByGoal := map[string]float64{}
	for _, s := range allSessions {
		sessionsByGoal[s.GoalID] += s.DurationMin
	}

	var total float64
	var walk func(id string)
	visited := map[string]bool{}
	walk = func(id string) {
		if visited[id] {
			return // guard against accidental cycles
		}
		visited[id] = true
		total += sessionsByGoal[id]
		for _, child := range childrenByParent[id] {
			walk(child.ID)
		}
	}
	walk(goal.ID)

	p.ActualMin = total
	p.Percent = (total / goal.TargetValue) * 100
	p.HasPercent = true
	return p
}

// TreeNode is a Goal flattened into display order with its depth, so
// templates can render indentation with a single range loop instead of
// recursive template calls (html/template has no easy recursion).
type TreeNode struct {
	Goal  models.Goal
	Depth int
}

// SortGoalsForTree flattens the goal tree into a parent-before-children,
// alphabetical-among-siblings order with depth info for indentation.
func SortGoalsForTree(goalsList []models.Goal) []TreeNode {
	byParent := map[string][]models.Goal{}
	for _, g := range goalsList {
		byParent[g.ParentGoalID] = append(byParent[g.ParentGoalID], g)
	}
	for k := range byParent {
		sort.Slice(byParent[k], func(i, j int) bool {
			return byParent[k][i].Title < byParent[k][j].Title
		})
	}
	var out []TreeNode
	var walk func(parentID string, depth int)
	walk = func(parentID string, depth int) {
		for _, g := range byParent[parentID] {
			out = append(out, TreeNode{Goal: g, Depth: depth})
			walk(g.ID, depth+1)
		}
	}
	walk("", 0)
	return out
}
