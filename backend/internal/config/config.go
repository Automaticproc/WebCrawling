package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr          string
	MySQLDSN      string
	AdminUser     string
	AdminPassword string
	SessionSecret string
	WebOrigin     string
	SyncDelay     time.Duration
	CookieSecure  bool

	DownloadDir         string
	FFmpegPath          string
	BrowserPath         string
	BrowserPort         int
	BrowserProfile      string
	BrowserCookie       string
	BrowserTimeout      time.Duration
	DownloadConcurrency int

	LoginURL            string
	LoginUser           string
	LoginPassword       string
	LoginUserSelector   string
	LoginPassSelector   string
	LoginSubmitSelector string
}

func Load() Config {
	delay := 400
	if v := strings.TrimSpace(os.Getenv("SYNC_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			delay = n
		}
	}
	browserTimeout := 45
	if v := strings.TrimSpace(os.Getenv("DOWNLOAD_BROWSER_TIMEOUT_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			browserTimeout = n
		}
	}
	browserPort := 0
	if v := strings.TrimSpace(os.Getenv("DOWNLOAD_BROWSER_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			browserPort = n
		}
	}
	concurrency := 8
	if v := strings.TrimSpace(os.Getenv("DOWNLOAD_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}
	return Config{
		Addr:          getenv("APP_ADDR", ":43123"),
		MySQLDSN:      normalizeDSN(getenv("MYSQL_DSN", "catalog:catalog@tcp(127.0.0.1:3306)/catalog?parseTime=true&charset=utf8mb4&loc=UTC")),
		AdminUser:     getenv("ADMIN_USERNAME", "admin"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		WebOrigin:     strings.TrimRight(getenv("WEB_ORIGIN", "http://127.0.0.1:43124"), "/"),
		SyncDelay:     time.Duration(delay) * time.Millisecond,
		CookieSecure:  os.Getenv("COOKIE_SECURE") == "1" || strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true"),

		DownloadDir:         getenv("DOWNLOAD_DIR", "downloads"),
		FFmpegPath:          strings.TrimSpace(os.Getenv("FFMPEG_PATH")),
		BrowserPath:         strings.TrimSpace(os.Getenv("DOWNLOAD_BROWSER")),
		BrowserPort:         browserPort,
		BrowserProfile:      strings.TrimSpace(os.Getenv("DOWNLOAD_BROWSER_PROFILE")),
		BrowserCookie:       strings.TrimSpace(os.Getenv("DOWNLOAD_COOKIE")),
		BrowserTimeout:      time.Duration(browserTimeout) * time.Second,
		DownloadConcurrency: concurrency,

		LoginURL:            strings.TrimSpace(os.Getenv("DOWNLOAD_LOGIN_URL")),
		LoginUser:           strings.TrimSpace(os.Getenv("DOWNLOAD_USERNAME")),
		LoginPassword:       os.Getenv("DOWNLOAD_PASSWORD"),
		LoginUserSelector:   strings.TrimSpace(os.Getenv("DOWNLOAD_LOGIN_USER_SELECTOR")),
		LoginPassSelector:   strings.TrimSpace(os.Getenv("DOWNLOAD_LOGIN_PASSWORD_SELECTOR")),
		LoginSubmitSelector: strings.TrimSpace(os.Getenv("DOWNLOAD_LOGIN_SUBMIT_SELECTOR")),
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func normalizeDSN(dsn string) string {
	if !strings.Contains(dsn, "parseTime=") {
		dsn += "&parseTime=true"
	}
	if !strings.Contains(dsn, "charset=") {
		dsn += "&charset=utf8mb4"
	}
	if !strings.Contains(dsn, "loc=") {
		dsn += "&loc=UTC"
	}
	if !strings.Contains(dsn, "clientFoundRows=") {
		dsn += "&clientFoundRows=true"
	}
	if strings.Contains(dsn, "?") {
		return dsn
	}
	return strings.Replace(dsn, "&", "?", 1)
}
