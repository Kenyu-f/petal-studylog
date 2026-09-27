// Command studylog runs the StudyLog server.
//
// See explanation.md for the full design writeup. Quick start:
//
//	go run .
//	open http://localhost:8080
//
// Cloud deployment: most PaaS platforms (Railway, Fly.io, Render, ...)
// inject a $PORT environment variable and expect the app to listen on
// it, and give you a mounted volume at some path for persistent data.
// -addr/-data flags (for local, explicit control) take priority; if
// omitted, $PORT and $DATA_PATH are used as a fallback so the same
// binary works unmodified in both environments. See explanation.md,
// "Deploying StudyLog".
package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"

	"studylog/internal/handlers"
	"studylog/internal/store"
)

func main() {
	addrFlag := flag.String("addr", "", "listen address, e.g. :8080 (falls back to $PORT, then :8080)")
	dataFlag := flag.String("data", "", "path to the data file (falls back to $DATA_PATH, then data/studylog.json)")
	flag.Parse()

	addr := *addrFlag
	if addr == "" {
		if p := os.Getenv("PORT"); p != "" {
			addr = ":" + p
		} else {
			addr = ":8080"
		}
	}

	dataPath := *dataFlag
	if dataPath == "" {
		if d := os.Getenv("DATA_PATH"); d != "" {
			dataPath = d
		} else {
			dataPath = "data/studylog.json"
		}
	}

	s, err := store.Open(dataPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"pct": func(ratio float64) int { return int(ratio*100 + 0.5) },
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}).ParseGlob("templates/*.html")
	if err != nil {
		log.Fatalf("failed to parse templates: %v", err)
	}

	app := handlers.New(s, tmpl)

	log.Printf("StudyLog listening on %s (data file: %s)", addr, dataPath)
	if err := http.ListenAndServe(addr, app.Routes()); err != nil {
		log.Fatal(err)
	}
}
