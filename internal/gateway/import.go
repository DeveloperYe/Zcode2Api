package gateway

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ImportResult struct {
	Added   int      `json:"added"`
	Skipped int      `json:"skipped"`
	NoKey   int      `json:"no_key"`
	Total   int      `json:"total"`
	File    string   `json:"file,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

func sqliteRO(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file:" + filepath.ToSlash(abs) + "?mode=ro"
}

// maxImportBytes caps a manager-database upload (and the server-side file it
// points at). A manager DB is a few MB at most.
const maxImportBytes = 64 << 20

// importDir is the only directory a server-side import path may live in.
func importDir() (string, error) {
	dir, err := filepath.Abs(filepath.Join("data", "import"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create import dir: %w", err)
	}
	return dir, nil
}

// withinDir reports whether target is strictly inside dir (after resolving
// symlinks on both), i.e. no "..", no absolute-elsewhere, no symlink escape.
func withinDir(dir, target string) (string, bool) {
	dirReal, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	// resolve the file too; a symlink inside the dir must not point outside it
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false // missing file (or broken link) is not acceptable
	}
	rel, err := filepath.Rel(dirReal, real)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return real, true
}

// importFile resolves the manager database to read.
//
// Two forms are accepted, and nothing else:
//
//   - multipart upload (the normal path): streamed to a 0600 temp file
//   - {"path": "..."} where the path resolves inside data/import/
//
// The old behaviour — open any path the caller named, `os.Stat` it and let the
// SQLite error text leak back — turned this endpoint into a file-existence
// oracle over the whole machine (and a way to slurp any SQLite database on it).
func importFile(r *http.Request) (path, cleanup string, err error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(maxImportBytes); err != nil {
			return "", "", fmt.Errorf("upload: %w", err)
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return "", "", fmt.Errorf("pick a .db file")
		}
		defer f.Close()
		tmp, err := os.CreateTemp("", "zcode-mgr-*.db")
		if err != nil {
			return "", "", err
		}
		n, err := io.Copy(tmp, io.LimitReader(f, maxImportBytes+1))
		if err == nil && n > maxImportBytes {
			err = fmt.Errorf("file larger than %d bytes", maxImportBytes)
		}
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			return "", "", err
		}
		return tmp.Name(), tmp.Name(), nil
	}

	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	want := strings.TrimSpace(body.Path)
	if want == "" {
		return "", "", fmt.Errorf("upload a .db file, or pass a path inside data/import/")
	}
	dir, err := importDir()
	if err != nil {
		return "", "", err
	}
	// a bare filename is taken relative to data/import/
	if filepath.Base(want) == want {
		want = filepath.Join(dir, want)
	}
	real, ok := withinDir(dir, want)
	if !ok {
		return "", "", fmt.Errorf("path must be a regular file inside data/import/")
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.Mode().IsRegular() {
		return "", "", fmt.Errorf("path must be a regular file inside data/import/")
	}
	if fi.Size() > maxImportBytes {
		return "", "", fmt.Errorf("file larger than %d bytes", maxImportBytes)
	}
	return real, "", nil
}

func (s *Server) ImportManagerDB(path string) (ImportResult, error) {
	path = strings.TrimSpace(path)
	res := ImportResult{File: filepath.Base(path)}
	if path == "" {
		return res, fmt.Errorf("no file")
	}
	if _, err := os.Stat(path); err != nil {
		return res, fmt.Errorf("manager db: %w", err)
	}
	src, err := sql.Open("sqlite", sqliteRO(path))
	if err != nil {
		return res, err
	}
	defer src.Close()
	src.SetMaxOpenConns(1)

	rows, err := src.Query(`SELECT TRIM(COALESCE(email,'')), TRIM(COALESCE(zcodeApiKey,'')), COALESCE(id,'') FROM accounts`)
	if err != nil {
		// schema without the manager row id (fixtures/older exports): import
		// without the gateway device-mid pin
		rows, err = src.Query(`SELECT TRIM(COALESCE(email,'')), TRIM(COALESCE(zcodeApiKey,'')), '' FROM accounts`)
		if err != nil {
			return res, fmt.Errorf("not a manager accounts.db (need zcodeApiKey): %w", err)
		}
	}
	defer rows.Close()

	for rows.Next() {
		var email, key, rowID string
		if err := rows.Scan(&email, &key, &rowID); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		res.Total++
		if key == "" {
			res.NoKey++
			continue
		}
		if s.store.HasJWT(key) {
			res.Skipped++
			continue
		}
		if _, err := s.store.Add(key, email); err != nil {
			res.Skipped++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", email, err))
			continue
		}
		// The device identity is the manager gateway's mid for this account —
		// uuid5(DNS, "zcode-gw."+<manager row id>) — the (account, device)
		// pair the edge already knows. A fresh uuid instead is a brand-new
		// device and gets 405/3012 "unusual activity" (proven 2026-09-11:
		// same request + the real mid → 200).
		if rowID != "" {
			mid := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("zcode-gw."+rowID)).String()
			s.store.SetDeviceMid(key, mid)
		}
		res.Added++
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	return res, nil
}
