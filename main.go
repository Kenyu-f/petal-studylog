// Command studylog runs the StudyLog server.
//
// See explanation.md for the full design writeup. Quick start:
//
//	go run .
//	open http://localhost:8080
package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"

	"studylog/internal/handlers"
	"studylog/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataPath := flag.String("data", "data/studylog.json", "path to the data file")
	flag.Parse()

	s, err := store.Open(*dataPath)
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

	log.Printf("StudyLog listening on %s (data file: %s)", *addr, *dataPath)
	if err := http.ListenAndServe(*addr, app.Routes()); err != nil {
		log.Fatal(err)
	}
}
