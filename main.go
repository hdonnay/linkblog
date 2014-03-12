// Copyright 2013 Hank Donnay

// a linkblog
package main

import (
	"archive/zip"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	initSQL = `CREATE TABLE IF NOT EXISTS links (
		id INTEGER NOT NULL PRIMARY KEY,
		hash TEXT NOT NULL UNIQUE,
		desc TEXT,
		url TEXT,
		hits INTEGER,
		time TIMESTAMP);`
)

type record struct {
	Time time.Time
	Hash string
	Desc string
	Hits int64
}

type (
	Rss struct {
		XMLName  string    `xml:"rss"`
		Channels []Channel `xml:"channel"`
		Version  string    `xml:"version,attr"`
	}

	// Channel is an RSS Channel
	Channel struct {
		Docs          string
		Title         string `xml:"title"`
		Link          string `xml:"link"`
		Description   string `xml:"description"`
		Language      string `xml:"language"`
		WebMaster     string `xml:"webMaster,omitempty"`
		Generator     string `xml:"generator"`
		PubDate       string `xml:"pubDate"`
		LastBuildDate string `xml:"lastBuildDate"`
		Items         []Item `xml:"item"`
	}

	// Item is an RSS Item
	Item struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Author      string `xml:"author,omitempty"`
		Category    string `xml:"category,omitempty"`
		Comments    string `xml:"comments,omitempty"`
		GUID        string `xml:"guid,omitempty"`
		//PubDate     time.Time `xml:"pubDate"`
	}
)

var (
	assetDir string
	db       *sql.DB
	tmpl     *template.Template

	listen     = flag.String("l", "127.0.0.1:7990", "listen address")
	dbFile     = flag.String("d", "linkblog.db", "sqlite db")
	prettyAddr = flag.String("pretty", "", "pretty address for links. defaults to 'l' value")
	feedLimit  = flag.Int("feedlim", 50, "maximum number of items in the rss feed")
)

func init() {
	flag.Parse()
	if *prettyAddr == "" {
		*prettyAddr = "http://" + *listen
	}
	self, err := exec.LookPath(os.Args[0])
	if err != nil {
		log.Fatal(err)
	}
	assetDir, err = ioutil.TempDir("", path.Base(self))
	if err != nil {
		log.Fatal(err)
	}
	r, err := zip.OpenReader(self)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		aoPath := path.Join(assetDir, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.Mkdir(aoPath, 0700); err != nil {
				log.Fatal(err)
			}
			continue
		}
		ai, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}
		defer ai.Close()
		ao, err := os.Create(aoPath)
		if err != nil {
			log.Fatal(err)
		}
		defer ao.Close()
		_, err = io.Copy(ao, ai)
		if err != nil {
			log.Fatal(err)
		}
	}
	tmpl, err = template.ParseGlob(asset("tmpl/*"))
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	var err error
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, os.Kill)
	defer log.Println("exiting")
	defer os.RemoveAll(assetDir)

	db, err = sql.Open("sqlite3", *dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(initSQL)
	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/", http.HandlerFunc(index))
	http.Handle("/hits/", http.HandlerFunc(hits))
	http.Handle("/admin/add/", http.HandlerFunc(adminAdd))
	http.Handle("/rss/", http.HandlerFunc(rss))
	http.Handle("/:/", http.StripPrefix("/:/", http.HandlerFunc(fetch)))
	http.Handle("/s/", http.StripPrefix("/s/", http.FileServer(http.Dir(asset("static")))))

	go func() {
		log.Println("listening on " + *listen)
		http.ListenAndServe(*listen, nil)
	}()
	<-term
}

func asset(f string) string {
	return path.Join(assetDir, f)
}

func fetch(w http.ResponseWriter, r *http.Request) {
	var urlString string
	err := db.QueryRow("SELECT url FROM links WHERE hash=?;", r.URL.Path).Scan(&urlString)
	switch err {
	case nil:
		if _, err := db.Exec("UPDATE links SET hits=hits+1 WHERE hash=?;", r.URL.Path); err != nil {
			fmt.Println(err)
		}
		http.Redirect(w, r, urlString, http.StatusMovedPermanently)
	case sql.ErrNoRows:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "404 not found")
	default:
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
	}
	return
}

func adminAdd(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		// validation
		if err := r.ParseForm(); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.PostForm.Get("url") == "" || r.PostForm.Get("desc") == "" {
			w.WriteHeader(http.StatusBadRequest)
			err := tmpl.ExecuteTemplate(w, "add.html", "both fields are required")
			if err != nil {
				log.Println(err)
			}
			return
		}

		// mess with DB
		res, err := db.Exec("INSERT INTO links (hash, desc, url, time, hits) VALUES (?, ?, ?, ?, 0);",
			hash(r.PostForm.Get("url")), r.PostForm.Get("desc"), r.PostForm.Get("url"), time.Now().UTC())
		if err != nil {
			if err.Error() == "column hash is not unique" {
				w.WriteHeader(http.StatusConflict)
				if err := tmpl.ExecuteTemplate(w, "add.html", "url already exists"); err != nil {
					log.Println(err)
				}
				return
			}
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var h string
		id, _ := res.LastInsertId()
		if err := db.QueryRow("SELECT hash FROM links WHERE id=?;", id).Scan(&h); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// display
		w.WriteHeader(http.StatusSeeOther)
		err = tmpl.ExecuteTemplate(w, "added.html", h)
		if err != nil {
			log.Println(err)
		}
	case "GET":
		err := tmpl.ExecuteTemplate(w, "add.html", nil)
		if err != nil {
			log.Println(err)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
	return
}

func hash(s string) string {
	h := fnv.New32a()
	fmt.Fprint(h, s)
	return hex.EncodeToString(h.Sum(nil))
}

func index(w http.ResponseWriter, r *http.Request) {
	c := make(chan record, 10)
	go func() {
		rows, err := db.Query("SELECT time, hash, desc FROM links ORDER BY time DESC;")
		if err != nil {
			log.Println(err)
			close(c)
			return
		}
		for rows.Next() {
			var r record
			err := rows.Scan(&r.Time, &r.Hash, &r.Desc)
			if err != nil {
				log.Println(err)
				continue
			}
			c <- r
		}
		close(c)
	}()
	err := tmpl.ExecuteTemplate(w, "index.html", c)
	if err != nil {
		log.Println(err)
	}
	return
}

func hits(w http.ResponseWriter, r *http.Request) {
	c := make(chan record, 10)
	go func() {
		rows, err := db.Query("SELECT time, hash, desc, hits FROM links ORDER BY hits DESC;")
		if err != nil {
			log.Println(err)
			close(c)
			return
		}
		for rows.Next() {
			var r record
			err := rows.Scan(&r.Time, &r.Hash, &r.Desc, &r.Hits)
			if err != nil {
				log.Println(err)
				continue
			}
			c <- r
		}
		close(c)
	}()
	err := tmpl.ExecuteTemplate(w, "hits.html", c)
	if err != nil {
		log.Println(err)
	}
	return
}

func rss(w http.ResponseWriter, r *http.Request) {
	fi, err := os.Stat(asset("rss.xml"))
	if err != nil {
		if err := createRSS(asset("rss.xml")); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
		fi, _ = os.Stat(asset("rss.xml"))
	}
	if time.Since(fi.ModTime()) > (time.Duration(30) * time.Minute) {
		if err := createRSS(asset("rss.xml")); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
	}
	feed, err := os.Open(asset("rss.xml"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}
	etag, _ := ioutil.ReadFile(asset("rss.xml.etag"))
	w.Header().Add("Etag", hex.EncodeToString(etag))
	io.Copy(w, feed)
	return
}

func createRSS(f string) error {
	out, err := os.Create(f)
	if err != nil {
		return err
	}
	defer out.Close()
	h := fnv.New64a()
	w := io.MultiWriter(out, h)
	rows, err := db.Query("SELECT hash, desc FROM links ORDER BY time DESC LIMIT ?;", *feedLimit)
	if err != nil {
		return err
	}

	items := make([]Item, 0, *feedLimit)
	for rows.Next() {
		var i Item
		var h string
		err := rows.Scan(&h, &i.Description)
		if err != nil {
			log.Println(err)
			continue
		}
		i.Title = i.Description
		u, err := url.Parse(*prettyAddr + "/:/" + h)
		if err != nil {
			log.Println(err)
			continue
		}
		i.Link = u.String()
		items = append(items, i)
	}
	io.WriteString(w, xml.Header)
	e := xml.NewEncoder(w)
	now := time.Now().UTC().Format(time.RFC822)
	r := Rss{
		Version: "2.0",
		Channels: []Channel{Channel{
			Title:         "linkblog",
			Docs:          "http://blogs.law.harvard.edu/tech/rss",
			Language:      "en-us",
			PubDate:       now,
			LastBuildDate: now,
			Link:          fmt.Sprintf("%s/rss", *prettyAddr),
			Generator:     "github.com/hdonnay/linkblog",
			Items:         items},
		},
	}
	if err := e.Encode(r); err != nil {
		return err
	}
	ioutil.WriteFile(f+".etag", h.Sum(nil), 0600)
	return nil
}
