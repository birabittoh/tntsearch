package main

import (
	"embed"
	"encoding/csv"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

var (
	units = []string{"", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi"}

	//go:embed templates/*
	templateFS embed.FS
)

type Torrent struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Data        time.Time `gorm:"index" json:"data"`
	Hash        string    `gorm:"index" json:"hash"`
	Topic       string    `json:"topic"`
	Post        string    `json:"post"`
	Autore      string    `gorm:"index" json:"autore"`
	Titolo      string    `gorm:"index" json:"titolo"`
	Descrizione string    `json:"descrizione"`
	Dimensione  int64     `json:"dimensione"`
	Categoria   int       `gorm:"index" json:"categoria"`
}

var categorie = map[int]string{
	1:  "Film TV e programmi",
	2:  "Musica",
	3:  "E Books",
	4:  "Film",
	6:  "Linux",
	7:  "Anime",
	8:  "Cartoni",
	9:  "Macintosh",
	10: "Windows Software",
	11: "Pc Game",
	12: "Playstation",
	13: "Students Releases",
	14: "Documentari",
	21: "Video Musicali",
	22: "Sport",
	23: "Teatro",
	24: "Wrestling",
	25: "Varie",
	26: "Xbox",
	27: "Immagini sfondi",
	28: "Altri Giochi",
	29: "Serie TV",
	30: "Fumetteria",
	31: "Trash",
	32: "Nintendo",
	34: "A Book",
	35: "Podcast",
	36: "Edicola",
	37: "Mobile",
}

var tableHeaders = []string{"DATA", "CATEGORIA", "TITOLO", "DESCRIZIONE", "AUTORE", "DIMENSIONE", "HASH"}

type App struct {
	db *gorm.DB
}

func NewApp(db *gorm.DB) *App {
	return &App{db: db}
}

func (a *App) loadCSVData(csvPath string) error {
	file, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ','
	reader.TrimLeadingSpace = true

	// Skip header
	_, err = reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read CSV records: %w", err)
	}

	var torrents []Torrent
	for _, record := range records {
		if len(record) != 9 {
			continue // Skip malformed records
		}

		// Parse date
		data, err := time.Parse("2006-01-02T15:04:05", record[0])
		if err != nil {
			log.Printf("Error parsing date %s: %v", record[0], err)
			continue
		}

		// Parse dimensione
		dimensione, err := strconv.ParseInt(record[7], 10, 64)
		if err != nil {
			log.Printf("Error parsing dimensione %s: %v", record[7], err)
			continue
		}

		// Parse categoria
		categoria, err := strconv.Atoi(record[8])
		if err != nil {
			log.Printf("Error parsing categoria %s: %v", record[8], err)
			continue
		}

		torrent := Torrent{
			Data:        data,
			Hash:        record[1],
			Topic:       record[2],
			Post:        record[3],
			Autore:      record[4],
			Titolo:      record[5],
			Descrizione: record[6],
			Dimensione:  dimensione,
			Categoria:   categoria,
		}

		torrents = append(torrents, torrent)
	}

	// Batch insert for better performance
	batchSize := 1000
	for i := 0; i < len(torrents); i += batchSize {
		end := i + batchSize
		if end > len(torrents) {
			end = len(torrents)
		}

		if err := a.db.CreateInBatches(torrents[i:end], batchSize).Error; err != nil {
			return fmt.Errorf("failed to insert batch: %w", err)
		}

		log.Printf("Inserted batch %d-%d of %d torrents", i+1, end, len(torrents))
	}

	log.Printf("Successfully loaded %d torrents from CSV", len(torrents))
	return nil
}

func (a *App) searchTorrents(keywords string, category, page, pageSize int) ([]Torrent, error) {
	var torrents []Torrent
	query := a.db.Model(&Torrent{})

	if keywords != "" {
		kw := "%" + strings.ToLower(keywords) + "%"
		query = query.Where("LOWER(titolo) LIKE ? OR LOWER(descrizione) LIKE ? OR LOWER(autore) LIKE ?", kw, kw, kw)
	}

	if category != 0 {
		query = query.Where("categoria = ?", category)
	}

	offset := (page - 1) * pageSize
	err := query.Order("data DESC").Limit(pageSize).Offset(offset).Find(&torrents).Error

	return torrents, err
}

func sizeofFmt(num int64) string {
	if num == 0 {
		return "0B"
	}

	fnum := float64(num)
	for _, unit := range units {
		if fnum < 1024.0 {
			return fmt.Sprintf("%.1f%sB", fnum, unit)
		}
		fnum /= 1024.0
	}
	return fmt.Sprintf("%.1f%sB", fnum, "Yi")
}

func formatTorrent(t Torrent) []any {
	return []any{
		t.Data.Format("02/01/2006"),
		template.HTML(fmt.Sprintf(`<a href="?keywords=&category=%d&page=1">%s</a>`, t.Categoria, categorie[t.Categoria])),
		t.Titolo,
		t.Descrizione,
		template.HTML(fmt.Sprintf(`<a href="?keywords=%s&category=0&page=1">%s</a>`, t.Autore, t.Autore)),
		sizeofFmt(t.Dimensione),
		template.HTML(fmt.Sprintf(`<a href="magnet:?xt=urn:btih:%s" class="magnet-btn">🧲</a>`, t.Hash)),
	}
}

func getArgs(r *http.Request) (string, int, int) {
	keywords := r.URL.Query().Get("keywords")

	category, _ := strconv.Atoi(r.URL.Query().Get("category"))

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	return keywords, category, page
}

func (a *App) handleMain(w http.ResponseWriter, r *http.Request) {
	keywords, category, page := getArgs(r)

	torrents, err := a.searchTorrents(keywords, category, page, 50)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		log.Printf("Database error: %v", err)
		return
	}

	var formattedResults [][]any
	for _, torrent := range torrents {
		formattedResults = append(formattedResults, formatTorrent(torrent))
	}

	// Convert map to slice of key-value pairs for template
	var categoriesList []struct {
		Key   int
		Value string
	}
	for k, v := range categorie {
		categoriesList = append(categoriesList, struct {
			Key   int
			Value string
		}{k, v})
	}

	data := struct {
		Headers    []string
		Content    [][]any
		Categories []struct {
			Key   int
			Value string
		}
		Page int
	}{
		Headers:    tableHeaders,
		Content:    formattedResults,
		Categories: categoriesList,
		Page:       page,
	}

	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func (a *App) handleAPIHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	headers := []string{"DATA", "HASH", "TOPIC", "POST", "AUTORE", "TITOLO", "DESCRIZIONE", "DIMENSIONE", "CATEGORIA"}

	response := `["` + strings.Join(headers, `","`) + `"]`
	w.Write([]byte(response))
}

func (a *App) handleAPI(w http.ResponseWriter, r *http.Request) {
	keywords, category, page := getArgs(r)

	torrents, err := a.searchTorrents(keywords, category, page, 50)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		log.Printf("Database error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Simple JSON serialization
	var jsonResults []string
	for _, t := range torrents {
		jsonResult := fmt.Sprintf(`{
			"data": "%s",
			"hash": "%s",
			"topic": "%s",
			"post": "%s",
			"autore": "%s",
			"titolo": "%s",
			"descrizione": "%s",
			"dimensione": %d,
			"categoria": %d
		}`,
			t.Data.Format("2006-01-02T15:04:05"),
			t.Hash,
			strings.ReplaceAll(t.Topic, `"`, `\"`),
			strings.ReplaceAll(t.Post, `"`, `\"`),
			strings.ReplaceAll(t.Autore, `"`, `\"`),
			strings.ReplaceAll(t.Titolo, `"`, `\"`),
			strings.ReplaceAll(t.Descrizione, `"`, `\"`),
			t.Dimensione,
			t.Categoria,
		)
		jsonResults = append(jsonResults, jsonResult)
	}

	response := "[" + strings.Join(jsonResults, ",") + "]"
	w.Write([]byte(response))
}

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/tntsearch.db"
	}

	csvPath := os.Getenv("CSV_PATH")
	if csvPath == "" {
		csvPath = "tntvillage-release-dump/tntvillage-release-dump.csv"
	}

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = ":3000"
	}

	// Ensure db directory exists
	dbDir := dbPath[:strings.LastIndex(dbPath, "/")]
	if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
		log.Fatal("Failed to create db directory:", err)
	}

	// Initialize database
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Auto migrate the schema
	if err := db.AutoMigrate(&Torrent{}); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	app := NewApp(db)

	// Check if we need to load data
	var count int64
	db.Model(&Torrent{}).Count(&count)
	if count == 0 {
		log.Println("Database is empty, loading CSV data...")
		if err := app.loadCSVData(csvPath); err != nil {
			log.Printf("Warning: Failed to load CSV data: %v", err)
		}
	} else {
		log.Printf("Database already contains %d torrents", count)
	}

	// Setup routes
	http.HandleFunc("/", app.handleMain)
	http.HandleFunc("/api/header", app.handleAPIHeader)
	http.HandleFunc("/api", app.handleAPI)

	log.Printf("Server starting on %s", address)
	log.Fatal(http.ListenAndServe(address, nil))
}
