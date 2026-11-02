// Package catalog loads the demo product catalog.
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
)

type Product struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Brand        string   `json:"brand"`
	PriceINR     int      `json:"price_inr"`
	Claim        string   `json:"claim"`
	Target       string   `json:"target"`       // the concern this product is judged on
	Guards       []string `json:"guards"`       // concerns also scanned, to catch side effects
	Alternatives []string `json:"alternatives"` // swap candidates when NotWorking
}

type Catalog struct {
	Products      []Product         `json:"products"`
	ConcernLabels map[string]string `json:"concern_labels"`
	byID          map[string]Product
}

func Load(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	c.byID = map[string]Product{}
	for _, p := range c.Products {
		c.byID[p.ID] = p
	}
	return &c, nil
}

func (c *Catalog) Product(id string) (Product, bool) {
	p, ok := c.byID[id]
	return p, ok
}

// Concerns returns target + guards for a product (what to scan each check-in).
func (c *Catalog) Concerns(p Product) []string {
	out := []string{p.Target}
	seen := map[string]bool{p.Target: true}
	for _, g := range p.Guards {
		if !seen[g] {
			out = append(out, g)
			seen[g] = true
		}
	}
	return out
}

func (c *Catalog) Label(concern string) string {
	if l, ok := c.ConcernLabels[concern]; ok {
		return l
	}
	return concern
}
