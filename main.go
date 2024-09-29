package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type URLShortener struct {
	db      *sql.DB
	baseURL string
	tmpl    *template.Template
}

func NewURLShortener(baseURL string) *URLShortener {
	db, err := sql.Open("sqlite3", "urls.db")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS urls (
			short_code TEXT PRIMARY KEY,
			long_url TEXT NOT NULL,
			visits INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	tmpl, err := template.ParseFiles("templates/index.html", "templates/manage.html")
	if err != nil {
		log.Fatal(err)
	}

	return &URLShortener{
		db:      db,
		baseURL: baseURL,
		tmpl:    tmpl,
	}
}

func (us *URLShortener) Shorten(longURL string) (string, error) {
	// Follow redirects and get the final URL
	resp, err := http.Get(longURL)
	if err != nil {
		log.Printf("error fetching URL: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()

	shortCode := generateShortCode()
	_, err = us.db.Exec("INSERT INTO urls (short_code, long_url) VALUES (?, ?)", shortCode, finalURL)
	if err != nil {
		log.Printf("error inserting URL: %v", err)
		return "", err
	}
	Log(fmt.Sprintf("Added new URL: %s -> %s (original: %s)", shortCode, finalURL, longURL))
	return us.baseURL + shortCode, nil
}

func (us *URLShortener) Redirect(shortCode string) (string, bool) {
	var longURL string
	err := us.db.QueryRow("SELECT long_url FROM urls WHERE short_code = ?", shortCode).Scan(&longURL)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false
		}
		log.Printf("error querying URL: %v", err)
		return "", false
	}

	_, err = us.db.Exec("UPDATE urls SET visits = visits + 1 WHERE short_code = ?", shortCode)
	if err != nil {
		log.Printf("error updating visit count: %v", err)
	}

	return longURL, true
}

func (us *URLShortener) GetVisits(shortCode string) int {
	var visits int
	err := us.db.QueryRow("SELECT visits FROM urls WHERE short_code = ?", shortCode).Scan(&visits)
	if err != nil {
		log.Printf("error querying visits: %v", err)
		return 0
	}
	return visits
}

func (us *URLShortener) GetAllLinks() ([]Link, error) {
	rows, err := us.db.Query("SELECT short_code, long_url, visits FROM urls")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var link Link
		err := rows.Scan(&link.ShortCode, &link.LongURL, &link.Visits)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

func generateShortCode() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const codeLength = 6

	shortCode := make([]byte, codeLength)
	for i := range shortCode {
		shortCode[i] = charset[rand.Intn(len(charset))]
	}
	return string(shortCode)
}

func main() {
	rand.Seed(time.Now().UnixNano())
	shortener := NewURLShortener("http://localhost:8080/")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			shortener.tmpl.Execute(w, nil)
			return
		}

		shortCode := r.URL.Path[1:]
		longURL, exists := shortener.Redirect(shortCode)
		if !exists {
			http.NotFound(w, r)
			return
		}

		http.Redirect(w, r, longURL, http.StatusFound)
	})

	http.HandleFunc("/shorten", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		longURL := r.FormValue("url")
		if longURL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}

		shortURL, err := shortener.Shorten(longURL)
		if err != nil {
			http.Error(w, "Error shortening URL", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, shortURL)
	})

	http.HandleFunc("/manage", shortener.manageHandler)

	http.HandleFunc("/visits/", func(w http.ResponseWriter, r *http.Request) {
		shortCode := r.URL.Path[len("/visits/"):]
		visits := shortener.GetVisits(shortCode)
		fmt.Fprintf(w, "Visits for %s: %d\n", shortCode, visits)
	})

	Log("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func Log(message string) {
	log.Printf("Log: %s", message)
}

type Link struct {
	ShortCode string
	LongURL   string
	Visits    int
}

func (us *URLShortener) manageHandler(w http.ResponseWriter, r *http.Request) {
	links, err := us.GetAllLinks()
	if err != nil {
		log.Printf("Error getting links: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Debug log
	log.Printf("Number of links retrieved: %d", len(links))
	for _, link := range links {
		log.Printf("Link: %+v", link)
	}

	w.Header().Set("Content-Type", "text/html")
	err = us.tmpl.ExecuteTemplate(w, "manage.html", links)
	if err != nil {
		log.Printf("error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
