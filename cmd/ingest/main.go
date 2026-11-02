// ingest: feed dated historical selfies into a trial so a 4-week timeline can be
// built from photos you already have. Dates come from a manifest (one
// `path,YYYY-MM-DD` per line) or from each file's modification time.
//
//	go run ./cmd/ingest -server http://localhost:8080 -trial <id> -manifest photos.csv
//	go run ./cmd/ingest -server http://localhost:8080 -trial <id> a.jpg b.jpg   # uses mtime
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "skinverdict server")
	trial := flag.String("trial", "", "trial id (required)")
	manifest := flag.String("manifest", "", "csv: path,YYYY-MM-DD[THH:MM]")
	flag.Parse()
	if *trial == "" {
		log.Fatal("-trial required")
	}
	type item struct {
		path string
		at   time.Time
	}
	var items []item
	if *manifest != "" {
		f, err := os.Open(*manifest)
		if err != nil {
			log.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			p, d, ok := strings.Cut(line, ",")
			if !ok {
				log.Fatalf("bad manifest line: %q", line)
			}
			d = strings.TrimSpace(d)
			t, err := time.ParseInLocation("2006-01-02T15:04", d, time.Local)
			if err != nil {
				t, err = time.ParseInLocation("2006-01-02", d, time.Local)
			}
			if err != nil {
				log.Fatalf("bad date %q", d)
			}
			items = append(items, item{filepath.Join(filepath.Dir(*manifest), strings.TrimSpace(p)), t})
		}
	} else {
		for _, p := range flag.Args() {
			st, err := os.Stat(p)
			if err != nil {
				log.Fatal(err)
			}
			items = append(items, item{p, st.ModTime()})
		}
	}
	for _, it := range items {
		img, err := os.ReadFile(it.path)
		if err != nil {
			log.Fatal(err)
		}
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("image", filepath.Base(it.path))
		fw.Write(img)
		mw.WriteField("at", it.at.Format(time.RFC3339))
		mw.Close()
		res, err := http.Post(fmt.Sprintf("%s/api/trials/%s/scans", *server, *trial), mw.FormDataContentType(), &buf)
		if err != nil {
			log.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 300 {
			log.Fatalf("%s: %d %s", it.path, res.StatusCode, body)
		}
		fmt.Printf("ok %s @ %s\n", it.path, it.at.Format("2006-01-02"))
	}
}
