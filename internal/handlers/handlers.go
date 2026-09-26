// Package handlers wires HTTP requests to the store + grass packages.
//
// Routing uses Go 1.22's stdlib http.ServeMux pattern matching
// ("METHOD /path/{param}") so no third-party router is needed — see
// explanation.md section 15 ("API / Server Flow") for why.
package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"studylog/internal/authutil"
	"studylog/internal/grass"
	"studylog/internal/models"
	"studylog/internal/store"
)

const sessionCookieName = "studylog_session"

type App struct {
	Store *store.Store
	Tmpl  *template.Template
}

func New(s *store.Store, tmpl *template.Template) *App {
	return &App{Store: s, Tmpl: tmpl}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// ---------- routing ----------

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /setup", a.handleSetupPage)
	mux.HandleFunc("POST /setup", a.handleSetupSubmit)
	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLoginSubmit)
	mux.HandleFunc("POST /logout", a.handleLogout)
	mux.HandleFunc("GET /forgot-password", a.handleForgotPasswordPage)
	mux.HandleFunc("POST /forgot-password", a.handleForgotPasswordSubmit)

	// Pages (auth required)
	mux.HandleFunc("GET /{$}", a.withAuth(a.handleDashboard))
	mux.HandleFunc("GET /goals", a.withAuth(a.handleGoalsPage))

	// JSON API (auth required)
	mux.HandleFunc("GET /api/goals", a.withAuthJSON(a.apiListGoals))
	mux.HandleFunc("POST /api/goals", a.withAuthJSON(a.apiCreateGoal))
	mux.HandleFunc("PUT /api/goals/{id}", a.withAuthJSON(a.apiUpdateGoal))
	mux.HandleFunc("DELETE /api/goals/{id}", a.withAuthJSON(a.apiDeleteGoal))

	mux.HandleFunc("GET /api/grass", a.withAuthJSON(a.apiGrass))
	mux.HandleFunc("GET /api/day", a.withAuthJSON(a.apiDay))

	mux.HandleFunc("POST /api/sessions", a.withAuthJSON(a.apiCreateSession))
	mux.HandleFunc("PUT /api/sessions/{id}", a.withAuthJSON(a.apiUpdateSession))
	mux.HandleFunc("DELETE /api/sessions/{id}", a.withAuthJSON(a.apiDeleteSession))

	// Static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	return mux
}

// ---------- auth plumbing ----------

func (a *App) currentUser(r *http.Request) (models.User, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return models.User{}, false
	}
	uid, ok := a.Store.UserIDForToken(c.Value)
	if !ok {
		return models.User{}, false
	}
	u, err := a.Store.UserByID(uid)
	if err != nil {
		return models.User{}, false
	}
	return u, true
}

// withAuth redirects to /login (or /setup) for page requests.
func (a *App) withAuth(next func(http.ResponseWriter, *http.Request, models.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Store.AnyGroupExists() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		u, ok := a.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r, u)
	}
}

// withAuthJSON returns 401 JSON instead of redirecting, for API calls.
func (a *App) withAuthJSON(next func(http.ResponseWriter, *http.Request, models.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.currentUser(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next(w, r, u)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ---------- setup (first run) ----------

func (a *App) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if a.Store.AnyGroupExists() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.render(w, "setup.html", map[string]any{"Title": "Set up StudyLog"})
}

func (a *App) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if a.Store.AnyGroupExists() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad form")
		return
	}
	groupName := r.FormValue("groupName")
	u1name, u1user, u1pass := r.FormValue("user1DisplayName"), r.FormValue("user1Username"), r.FormValue("user1Password")
	u2name, u2user, u2pass := r.FormValue("user2DisplayName"), r.FormValue("user2Username"), r.FormValue("user2Password")

	if groupName == "" || u1user == "" || u1pass == "" || u2user == "" || u2pass == "" {
		a.render(w, "setup.html", map[string]any{"Title": "Set up StudyLog", "Error": "全ての必須項目を入力してください。"})
		return
	}

	group := models.Group{ID: newID("grp"), Name: groupName, CreatedAt: time.Now().UTC()}
	if err := a.Store.CreateGroup(group); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type recoveryItem struct{ Label, Code string }
	var items []recoveryItem

	for _, spec := range []struct{ display, username, password string }{
		{u1name, u1user, u1pass},
		{u2name, u2user, u2pass},
	} {
		salt, _ := authutil.NewSalt()
		recoveryCode, err := authutil.NewRecoveryCode()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recoverySalt, _ := authutil.NewSalt()
		u := models.User{
			ID:               newID("usr"),
			Username:         spec.username,
			DisplayName:      displayOr(spec.display, spec.username),
			PasswordHash:     authutil.HashPassword(spec.password, salt),
			Salt:             salt,
			RecoveryCodeHash: authutil.HashPassword(recoveryCode, recoverySalt),
			RecoveryCodeSalt: recoverySalt,
			GroupID:          group.ID,
			CreatedAt:        time.Now().UTC(),
		}
		if err := a.Store.CreateUser(u); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, recoveryItem{Label: u.DisplayName + " (" + u.Username + ")", Code: recoveryCode})
	}

	a.render(w, "recovery_codes.html", map[string]any{
		"Title":    "Save your recovery codes",
		"Heading":  "リカバリーコードを保存してください",
		"Subtitle": "パスワードを忘れた場合、このコードだけが再設定の手段になります。メール機能は無いため、二人ともスクリーンショットやメモアプリに保存しておくことを強くおすすめします。",
		"Items":    items,
		"NextURL":  "/login",
		"NextLabel": "保存しました。ログインへ進む",
	})
}

func displayOr(display, fallback string) string {
	if display != "" {
		return display
	}
	return fallback
}

// ---------- login/logout ----------

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !a.Store.AnyGroupExists() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", map[string]any{"Title": "Log in"})
}

func (a *App) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad form")
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	u, err := a.Store.UserByUsername(username)
	if err != nil || !authutil.VerifyPassword(password, u.Salt, u.PasswordHash) {
		a.render(w, "login.html", map[string]any{"Title": "Log in", "Error": "ユーザー名またはパスワードが違います。"})
		return
	}
	token, err := authutil.NewSessionToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.Store.PutToken(token, u.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 90, // 90 days; MVP has no "remember me" toggle
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = a.Store.DeleteToken(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------- forgot password ----------

func (a *App) handleForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	if !a.Store.AnyGroupExists() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	a.render(w, "forgot_password.html", map[string]any{"Title": "Reset password"})
}

func (a *App) handleForgotPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad form")
		return
	}
	username := r.FormValue("username")
	recoveryCode := strings.ToUpper(strings.TrimSpace(r.FormValue("recoveryCode")))
	newPassword := r.FormValue("newPassword")
	confirmPassword := r.FormValue("confirmPassword")

	fail := func(msg string) {
		a.render(w, "forgot_password.html", map[string]any{
			"Title": "Reset password", "Error": msg, "Username": username,
		})
	}

	if newPassword == "" || newPassword != confirmPassword {
		fail("新しいパスワードが一致しません。")
		return
	}
	if len(newPassword) < 8 {
		fail("パスワードは8文字以上にしてください。")
		return
	}

	u, err := a.Store.UserByUsername(username)
	if err != nil || u.RecoveryCodeHash == "" ||
		!authutil.VerifyPassword(recoveryCode, u.RecoveryCodeSalt, u.RecoveryCodeHash) {
		// Deliberately the same generic message whether the username or
		// the code was wrong, so a failed attempt can't be used to probe
		// which usernames exist.
		fail("ユーザー名またはリカバリーコードが正しくありません。")
		return
	}

	newSalt, _ := authutil.NewSalt()
	newRecoveryCode, err := authutil.NewRecoveryCode()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newRecoverySalt, _ := authutil.NewSalt()

	u.PasswordHash = authutil.HashPassword(newPassword, newSalt)
	u.Salt = newSalt
	// The used recovery code is retired and replaced, so it can't be
	// reused if it ever leaked (e.g. an old screenshot).
	u.RecoveryCodeHash = authutil.HashPassword(newRecoveryCode, newRecoverySalt)
	u.RecoveryCodeSalt = newRecoverySalt

	if err := a.Store.UpdateUser(u); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Also invalidate any existing "remember me" sessions for this user
	// as a basic precaution after a credential reset.
	// (MVP: tokens are only looked up by value, so we don't enumerate and
	// delete them here — a real DB version would add a userId index.)

	a.render(w, "recovery_codes.html", map[string]any{
		"Title":    "New recovery code",
		"Heading":  "パスワードを再設定しました",
		"Subtitle": "念のため、リカバリーコードも新しいものに更新しました。古いコードはもう使えません。新しいコードを保存してください。",
		"Items":    []struct{ Label, Code string }{{Label: u.DisplayName + " (" + u.Username + ")", Code: newRecoveryCode}},
		"NextURL":  "/login",
		"NextLabel": "保存しました。ログインへ進む",
	})
}

// ---------- pages ----------

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request, u models.User) {
	goalsList := a.Store.GoalsInGroup(u.GroupID)
	rateGoals := filterGoals(goalsList, func(g models.Goal) bool {
		return g.ProgressMode == models.ProgressRate && g.TargetPeriod == "day" && g.TargetValue > 0
	})
	sort.Slice(rateGoals, func(i, j int) bool { return rateGoals[i].Title < rateGoals[j].Title })

	partners := a.Store.UsersInGroup(u.GroupID)

	a.render(w, "dashboard.html", map[string]any{
		"Title":     "StudyLog",
		"Active":    "grass",
		"User":      u,
		"Partners":  partners,
		"RateGoals": rateGoals,
		"Today":     time.Now().Format("2006-01-02"),
	})
}

func (a *App) handleGoalsPage(w http.ResponseWriter, r *http.Request, u models.User) {
	goalsList := a.Store.GoalsInGroup(u.GroupID)
	sessions := a.Store.SessionsInGroup(u.GroupID)
	tree := grass.SortGoalsForTree(goalsList)

	type row struct {
		Node     grass.TreeNode
		Progress grass.Progress
		OwnerTag string
	}
	partners := a.Store.UsersInGroup(u.GroupID)
	nameByID := map[string]string{}
	for _, p := range partners {
		nameByID[p.ID] = p.DisplayName
	}

	rows := make([]row, 0, len(tree))
	for _, n := range tree {
		tag := "共有"
		if !n.Goal.Shared {
			tag = nameByID[n.Goal.OwnerID]
		}
		rows = append(rows, row{
			Node:     n,
			Progress: grass.ComputeProgress(n.Goal, goalsList, sessions),
			OwnerTag: tag,
		})
	}

	a.render(w, "goals.html", map[string]any{
		"Title":    "Goals",
		"Active":   "goals",
		"User":     u,
		"Rows":     rows,
		"AllGoals": goalsList,
	})
}

func filterGoals(in []models.Goal, keep func(models.Goal) bool) []models.Goal {
	var out []models.Goal
	for _, g := range in {
		if keep(g) {
			out = append(out, g)
		}
	}
	return out
}

// ---------- API: goals ----------

type goalInput struct {
	ParentGoalID string  `json:"parentGoalId"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	ProgressMode string  `json:"progressMode"`
	TargetValue  float64 `json:"targetValue"`
	TargetUnit   string  `json:"targetUnit"`
	TargetPeriod string  `json:"targetPeriod"`
	Shared       bool    `json:"shared"`
	Deadline     string  `json:"deadline"` // "YYYY-MM-DD" or ""
}

func (a *App) apiListGoals(w http.ResponseWriter, r *http.Request, u models.User) {
	writeJSON(w, http.StatusOK, a.Store.GoalsInGroup(u.GroupID))
}

func (a *App) apiCreateGoal(w http.ResponseWriter, r *http.Request, u models.User) {
	var in goalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}
	g := models.Goal{
		ID:           newID("goal"),
		GroupID:      u.GroupID,
		OwnerID:      u.ID,
		Shared:       in.Shared,
		ParentGoalID: in.ParentGoalID,
		Title:        in.Title,
		Description:  in.Description,
		Type:         models.GoalType(orDefault(in.Type, string(models.GoalTypeShortTerm))),
		ProgressMode: models.ProgressMode(orDefault(in.ProgressMode, string(models.ProgressNone))),
		TargetValue:  in.TargetValue,
		TargetUnit:   in.TargetUnit,
		TargetPeriod: orDefault(in.TargetPeriod, "day"),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if in.Shared {
		g.OwnerID = ""
	}
	if d, err := time.Parse("2006-01-02", in.Deadline); err == nil {
		g.Deadline = &d
	}
	if err := a.Store.CreateGoal(g); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (a *App) apiUpdateGoal(w http.ResponseWriter, r *http.Request, u models.User) {
	id := r.PathValue("id")
	existing, err := a.Store.GoalByID(id)
	if err != nil || existing.GroupID != u.GroupID {
		writeJSONError(w, http.StatusNotFound, "goal not found")
		return
	}
	var in goalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	existing.ParentGoalID = in.ParentGoalID
	existing.Title = in.Title
	existing.Description = in.Description
	existing.Type = models.GoalType(orDefault(in.Type, string(existing.Type)))
	existing.ProgressMode = models.ProgressMode(orDefault(in.ProgressMode, string(existing.ProgressMode)))
	existing.TargetValue = in.TargetValue
	existing.TargetUnit = in.TargetUnit
	existing.TargetPeriod = orDefault(in.TargetPeriod, existing.TargetPeriod)
	existing.Shared = in.Shared
	if in.Shared {
		existing.OwnerID = ""
	} else if existing.OwnerID == "" {
		existing.OwnerID = u.ID
	}
	existing.Deadline = nil
	if d, err := time.Parse("2006-01-02", in.Deadline); err == nil {
		existing.Deadline = &d
	}
	if err := a.Store.UpdateGoal(existing); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *App) apiDeleteGoal(w http.ResponseWriter, r *http.Request, u models.User) {
	id := r.PathValue("id")
	existing, err := a.Store.GoalByID(id)
	if err != nil || existing.GroupID != u.GroupID {
		writeJSONError(w, http.StatusNotFound, "goal not found")
		return
	}
	if len(a.Store.ChildGoals(id)) > 0 {
		writeJSONError(w, http.StatusConflict, "delete or move child goals first")
		return
	}
	if err := a.Store.DeleteGoal(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ---------- API: grass ----------

func dateRange(start, end string) ([]string, error) {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return nil, err
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, err
	}
	if e.Before(s) {
		return nil, errors.New("end before start")
	}
	var out []string
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out, nil
}

func (a *App) apiGrass(w http.ResponseWriter, r *http.Request, u models.User) {
	goalID := r.URL.Query().Get("goalId")
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	goal, err := a.Store.GoalByID(goalID)
	if err != nil || goal.GroupID != u.GroupID {
		writeJSONError(w, http.StatusNotFound, "goal not found")
		return
	}
	dates, err := dateRange(start, end)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid start/end")
		return
	}
	sessions := a.Store.SessionsInGroup(u.GroupID)
	cells := grass.BuildCells(goal, sessions, dates)
	writeJSON(w, http.StatusOK, map[string]any{
		"goal":  goal,
		"cells": cells,
	})
}

// ---------- API: day detail ----------

func (a *App) apiDay(w http.ResponseWriter, r *http.Request, u models.User) {
	date := r.URL.Query().Get("date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date")
		return
	}
	sessions := a.Store.SessionsForDate(u.GroupID, date)
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.Before(sessions[j].CreatedAt) })

	partners := a.Store.UsersInGroup(u.GroupID)
	nameByID := map[string]string{}
	for _, p := range partners {
		nameByID[p.ID] = p.DisplayName
	}
	type sessOut struct {
		models.StudySession
		UserDisplayName string `json:"userDisplayName"`
		GoalTitle       string `json:"goalTitle"`
	}
	out := make([]sessOut, 0, len(sessions))
	for _, s := range sessions {
		goalTitle := ""
		if s.GoalID != "" {
			if g, err := a.Store.GoalByID(s.GoalID); err == nil {
				goalTitle = g.Title
			}
		}
		out = append(out, sessOut{StudySession: s, UserDisplayName: nameByID[s.UserID], GoalTitle: goalTitle})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":     date,
		"sessions": out,
	})
}

// ---------- API: sessions ----------

type sessionInput struct {
	Date        string  `json:"date"`
	DurationMin float64 `json:"durationMin"`
	Subject     string  `json:"subject"`
	GoalID      string  `json:"goalId"`
	Description string  `json:"description"`
}

func (a *App) apiCreateSession(w http.ResponseWriter, r *http.Request, u models.User) {
	var in sessionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date")
		return
	}
	if in.DurationMin < 0 {
		writeJSONError(w, http.StatusBadRequest, "duration must be >= 0")
		return
	}
	if in.DurationMin == 0 && in.Description == "" {
		writeJSONError(w, http.StatusBadRequest, "record either a duration or a description")
		return
	}
	if in.GoalID != "" {
		if g, err := a.Store.GoalByID(in.GoalID); err != nil || g.GroupID != u.GroupID {
			writeJSONError(w, http.StatusBadRequest, "unknown goal")
			return
		}
	}
	sess := models.StudySession{
		ID:          newID("sess"),
		UserID:      u.ID,
		GroupID:     u.GroupID,
		Date:        in.Date,
		DurationMin: in.DurationMin,
		Subject:     in.Subject,
		GoalID:      in.GoalID,
		Description: in.Description,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := a.Store.CreateSession(sess); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (a *App) apiUpdateSession(w http.ResponseWriter, r *http.Request, u models.User) {
	id := r.PathValue("id")
	existing, err := a.Store.SessionByID(id)
	if err != nil || existing.GroupID != u.GroupID {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if existing.UserID != u.ID {
		writeJSONError(w, http.StatusForbidden, "cannot edit a partner's session")
		return
	}
	var in sessionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date")
		return
	}
	if in.DurationMin == 0 && in.Description == "" {
		writeJSONError(w, http.StatusBadRequest, "record either a duration or a description")
		return
	}
	existing.Date = in.Date
	existing.DurationMin = in.DurationMin
	existing.Subject = in.Subject
	existing.GoalID = in.GoalID
	existing.Description = in.Description
	if err := a.Store.UpdateSession(existing); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *App) apiDeleteSession(w http.ResponseWriter, r *http.Request, u models.User) {
	id := r.PathValue("id")
	existing, err := a.Store.SessionByID(id)
	if err != nil || existing.GroupID != u.GroupID {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if existing.UserID != u.ID {
		writeJSONError(w, http.StatusForbidden, "cannot delete a partner's session")
		return
	}
	if err := a.Store.DeleteSession(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- rendering ----------

func (a *App) render(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error (%s): %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
