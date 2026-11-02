// seed: populate demo trials for the brand dashboard WITHOUT spending units.
// It reuses scans already in the DB: for each seeded trial it copies real scan
// rows (scores, images, masks) from an existing trial and nudges the target
// concern along a chosen trajectory. Seeded personas are prefixed "Demo:" so
// they are obviously synthetic in the UI.
//
//	go run ./cmd/seed -db data-local/skinverdict.db -from <trial id with ≥2 scans>
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/manasnikam/skinverdict/internal/catalog"
	"github.com/manasnikam/skinverdict/internal/store"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type plan struct {
	name, profile, product string
	deltas                 []float64 // target-concern change at day 7/14/28 relative to baseline
	adherence              string
}

func main() {
	dbPath := flag.String("db", "data-local/skinverdict.db", "sqlite db")
	catPath := flag.String("catalog", "data/catalog.json", "catalog")
	from := flag.String("from", "", "trial id whose scans are the template (required)")
	seed := flag.Int64("seed", 7, "rng seed")
	flag.Parse()
	if *from == "" {
		log.Fatal("-from required")
	}
	rng := rand.New(rand.NewSource(*seed))
	cat, err := catalog.Load(*catPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite3", "file:"+*dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.OpenSQL(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}
	tmpl, err := st.Scans(*from)
	if err != nil || len(tmpl) == 0 {
		log.Fatalf("template trial %s has no scans (%v)", *from, err)
	}
	base := tmpl[0]

	plans := []plan{
		{"Demo: Aarav", "oily", "nia10", []float64{2, 5, 9}, "directed"},
		{"Demo: Priya", "oily", "nia10", []float64{1, 1, 0}, "directed"}, // flat at horizon -> credit
		{"Demo: Rohan", "combination", "nia10", []float64{3, 6, 7}, "mostly"},
		{"Demo: Sneha", "dry", "nia10", []float64{-1, -3, -6}, "directed"}, // worse -> credit
		{"Demo: Kabir", "oily", "bha2", []float64{4, 7, 10}, "directed"},
		{"Demo: Meera", "dry", "bha2", []float64{0, -5, -7}, "directed"},
		{"Demo: Ishaan", "combination", "vitc15", []float64{2, 4, 6}, "directed"},
		{"Demo: Diya", "dry", "vitc15", []float64{1, 2, 2}, "skipped"},
		{"Demo: Arjun", "normal", "azel10", []float64{3, 6, 8}, "directed"},
		{"Demo: Zara", "oily", "cera", []float64{1, 1, 2}, "directed"},
	}
	day0 := time.Now().AddDate(0, 0, -28)
	for _, pl := range plans {
		p := &store.Persona{Name: pl.name, Profile: pl.profile}
		if err := st.UpsertPersona(p); err != nil {
			log.Fatal(err)
		}
		prod, _ := cat.Product(pl.product)
		concerns := cat.Concerns(prod)
		t := &store.Trial{PersonaID: p.ID, ProductID: pl.product, Target: prod.Target, Concerns: concerns, StartedAt: day0, NextCheck: time.Now().Add(time.Hour), Status: "active"}
		if err := st.CreateTrial(t); err != nil {
			log.Fatal(err)
		}
		// one baseline per concern per trial; capped so target gains aren't clamped away at 100
		bases := map[string]float64{}
		for _, c := range concerns {
			b, ok := base.Scores[c]
			if !ok {
				b = 60 + rng.Float64()*20
			}
			bases[c] = min(b, 85)
		}
		for i, d := range []int{0, 7, 14, 28} {
			src := tmpl[i%len(tmpl)]
			sc := &store.Scan{TrialID: t.ID, At: day0.AddDate(0, 0, d), Scores: map[string]float64{}, UIScores: map[string]float64{},
				ImagePath: src.ImagePath, MaskPaths: src.MaskPaths, Overall: src.Overall, SkinAge: src.SkinAge, RunMS: src.RunMS, Units: 0, Cached: true}
			for _, c := range concerns {
				b := bases[c]
				v := b + rng.NormFloat64()*1.2
				if c == prod.Target && i > 0 {
					v = b + pl.deltas[i-1] + rng.NormFloat64()*0.8
				}
				sc.Scores[c] = clamp(v)
				sc.UIScores[c] = clamp(v*0.5 + 45)
			}
			if i > 0 {
				sc.Adherence = pl.adherence
			}
			if err := st.AddScan(sc); err != nil {
				log.Fatal(err)
			}
		}
		fmt.Printf("seeded %-14s %-7s %s  Δ=%v\n", pl.name, pl.product, t.ID, pl.deltas)
	}
	fmt.Println("now run a check-in on each (UI button or POST /api/trials/{id}/checkin) to populate actions/credits.")
}

func clamp(v float64) float64 {
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return float64(int(v*10)) / 10
}
