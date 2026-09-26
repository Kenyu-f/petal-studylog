// Package store is the persistence layer of StudyLog.
//
// DECISION D1 (see explanation.md section 13 "Data Persistence" and the
// "Decisions" log): the spec asked for SQLite. In this build environment
// we do not have access to the Go module proxy or a vanity-import capable
// network path, so pulling in a CGO or pure-Go SQLite driver is not
// reliably reproducible. Rather than silently hack around that constraint
// (e.g. vendoring a driver in a fragile way), we implement a small
// dependency-free JSON-file store behind a Store struct whose method set
// is intentionally shaped like a tiny repository/DAO layer. Swapping the
// internals for database/sql + a real SQLite/Postgres driver later only
// touches this one file; internal/handlers and internal/grass never touch
// the file system directly. See explanation.md for the full writeup.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"studylog/internal/models"
)

var ErrNotFound = errors.New("not found")

// data is the entire persisted state. Kept as one struct so the whole
// thing can be loaded/saved atomically as a single JSON document.
type data struct {
	Users    []models.User         `json:"users"`
	Groups   []models.Group        `json:"groups"`
	Goals    []models.Goal         `json:"goals"`
	Sessions []models.StudySession `json:"sessions"`
	// Tokens maps a session cookie token -> userID. Kept in the same file
	// for simplicity; in a real DB this would be its own table with TTLs.
	Tokens map[string]string `json:"tokens"`
}

// Store is a thread-safe, file-backed store. All mutation methods persist
// to disk before returning (write-through), so a page reload never loses
// data, per spec section 14.
type Store struct {
	mu   sync.RWMutex
	path string
	d    data
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, d: data{Tokens: map[string]string{}}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked() // create an empty file
	}
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return err
	}
	if s.d.Tokens == nil {
		s.d.Tokens = map[string]string{}
	}
	return nil
}

// saveLocked writes the current state to disk. Caller must hold s.mu.
// Writes to a temp file then renames, so a crash mid-write can't corrupt
// the data file.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---- Users ----

func (s *Store) CreateUser(u models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Users = append(s.d.Users, u)
	return s.saveLocked()
}

func (s *Store) UserByUsername(username string) (models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.Username == username {
			return u, nil
		}
	}
	return models.User{}, ErrNotFound
}

func (s *Store) UserByID(id string) (models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.d.Users {
		if u.ID == id {
			return u, nil
		}
	}
	return models.User{}, ErrNotFound
}

// UpdateUser overwrites a user record by ID — used by the password-reset
// flow (new password hash + new recovery code hash).
func (s *Store) UpdateUser(u models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Users {
		if existing.ID == u.ID {
			s.d.Users[i] = u
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) UsersInGroup(groupID string) []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []models.User{}
	for _, u := range s.d.Users {
		if u.GroupID == groupID {
			out = append(out, u)
		}
	}
	return out
}

// ---- Groups ----

func (s *Store) CreateGroup(g models.Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Groups = append(s.d.Groups, g)
	return s.saveLocked()
}

func (s *Store) GroupByID(id string) (models.Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.d.Groups {
		if g.ID == id {
			return g, nil
		}
	}
	return models.Group{}, ErrNotFound
}

func (s *Store) AnyGroupExists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.d.Groups) > 0
}

// ---- Sessions (auth tokens) ----

func (s *Store) PutToken(token, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Tokens[token] = userID
	return s.saveLocked()
}

func (s *Store) UserIDForToken(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	uid, ok := s.d.Tokens[token]
	return uid, ok
}

func (s *Store) DeleteToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Tokens, token)
	return s.saveLocked()
}

// ---- Goals ----

func (s *Store) CreateGoal(g models.Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Goals = append(s.d.Goals, g)
	return s.saveLocked()
}

func (s *Store) UpdateGoal(g models.Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Goals {
		if existing.ID == g.ID {
			g.UpdatedAt = time.Now().UTC()
			s.d.Goals[i] = g
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) GoalByID(id string) (models.Goal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.d.Goals {
		if g.ID == id {
			return g, nil
		}
	}
	return models.Goal{}, ErrNotFound
}

func (s *Store) GoalsInGroup(groupID string) []models.Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []models.Goal{}
	for _, g := range s.d.Goals {
		if g.GroupID == groupID {
			out = append(out, g)
		}
	}
	return out
}

func (s *Store) ChildGoals(parentID string) []models.Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []models.Goal{}
	for _, g := range s.d.Goals {
		if g.ParentGoalID == parentID {
			out = append(out, g)
		}
	}
	return out
}

func (s *Store) DeleteGoal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, g := range s.d.Goals {
		if g.ID == id {
			s.d.Goals = append(s.d.Goals[:i], s.d.Goals[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// ---- Study Sessions ----

func (s *Store) CreateSession(sess models.StudySession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Sessions = append(s.d.Sessions, sess)
	return s.saveLocked()
}

func (s *Store) UpdateSession(sess models.StudySession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Sessions {
		if existing.ID == sess.ID {
			sess.UpdatedAt = time.Now().UTC()
			s.d.Sessions[i] = sess
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sess := range s.d.Sessions {
		if sess.ID == id {
			s.d.Sessions = append(s.d.Sessions[:i], s.d.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) SessionByID(id string) (models.StudySession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.d.Sessions {
		if sess.ID == id {
			return sess, nil
		}
	}
	return models.StudySession{}, ErrNotFound
}

func (s *Store) SessionsInGroup(groupID string) []models.StudySession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []models.StudySession{}
	for _, sess := range s.d.Sessions {
		if sess.GroupID == groupID {
			out = append(out, sess)
		}
	}
	return out
}

func (s *Store) SessionsForDate(groupID, date string) []models.StudySession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []models.StudySession{}
	for _, sess := range s.d.Sessions {
		if sess.GroupID == groupID && sess.Date == date {
			out = append(out, sess)
		}
	}
	return out
}
