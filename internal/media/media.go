package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/oklog/ulid/v2"
	_ "golang.org/x/image/webp"
)

const MaxUploadBytes = 10 << 20
const MaxFetchBytes = 15 << 20
const MaxPixels = 16_000_000

var imageWorkers = make(chan struct{}, 2)

type SQLStore interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type Store struct {
	DB      SQLStore
	Dir     string
	BaseURL string
}

type Item struct {
	UID       string `json:"uid"`
	URL       string `json:"url"`
	Mime      string `json:"mime"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Size      int    `json:"size"`
	Duplicate bool   `json:"duplicate"`
}

func (s *Store) URLFor(path string) string {
	return strings.TrimRight(s.BaseURL, "/") + "/media/" + path
}

// URLByUID returns the public URL for a stored media uid.
func (s *Store) URLByUID(uid string) (string, bool) {
	var p string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.DB.QueryRowContext(ctx, "SELECT path FROM media WHERE uid = ? AND status = 'active'", uid).Scan(&p)
	if err != nil {
		return "", false
	}
	return s.URLFor(p), true
}

// SaveBytes decodes, strips metadata by re-encoding, dedupes by sha256 and stores the file.
func (s *Store) SaveBytes(data []byte, sourceType string, clientID int64) (*Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.SaveBytesContext(ctx, data, sourceType, clientID)
}
func (s *Store) SaveBytesContext(ctx context.Context, data []byte, sourceType string, clientID int64) (*Item, error) {
	select {
	case imageWorkers <- struct{}{}:
		defer func() { <-imageWorkers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if len(data) == 0 {
		return nil, errors.New("empty file")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported image: %w", err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 10000 || cfg.Height > 10000 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, errors.New("image pixel budget exceeded")
	}
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("unsupported image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || w > 12000 || h > 12000 {
		return nil, errors.New("image dimensions out of range")
	}

	var buf bytes.Buffer
	var ext, mime string
	if hasAlpha(img) {
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		ext, mime = ".png", "image/png"
	} else {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
			return nil, err
		}
		ext, mime = ".jpg", "image/jpeg"
	}
	out := buf.Bytes()
	sum := sha256.Sum256(out)
	hash := hex.EncodeToString(sum[:])

	var uid, path string
	err = s.DB.QueryRowContext(ctx, "SELECT uid, path FROM media WHERE sha256 = ? AND status = 'active'", hash).Scan(&uid, &path)
	if err == nil {
		if ferr := s.ensureFile(path, out); ferr != nil {
			return nil, ferr
		}
		return &Item{UID: uid, URL: s.URLFor(path), Mime: mime, Width: w, Height: h, Size: len(out), Duplicate: true}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	rel := filepath.ToSlash(filepath.Join(time.Now().UTC().Format("2006/01"), hash+ext))
	full := filepath.Join(s.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	if err := writeAtomic(full, out); err != nil {
		return nil, err
	}

	uid = ulid.Make().String()
	var cid interface{}
	if clientID > 0 {
		cid = clientID
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO media (uid, sha256, path, mime, size, width, height, source_type, created_by_client_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, hash, rel, mime, len(out), w, h, sourceType, cid)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			// lost a race: reuse the winner
			if e2 := s.DB.QueryRowContext(ctx, "SELECT uid, path FROM media WHERE sha256 = ?", hash).Scan(&uid, &path); e2 == nil {
				if ferr := s.ensureFile(path, out); ferr != nil {
					return nil, ferr
				}
				return &Item{UID: uid, URL: s.URLFor(path), Mime: mime, Width: w, Height: h, Size: len(out), Duplicate: true}, nil
			}
		}
		return nil, err
	}
	return &Item{UID: uid, URL: s.URLFor(rel), Mime: mime, Width: w, Height: h, Size: len(out)}, nil
}

// Fetch downloads a remote image with SSRF protection and stores it.
func (s *Store) Fetch(ctx context.Context, raw string, clientID int64) (*Item, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("invalid url")
	}
	if err := checkHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if err := checkHost(req.Context(), req.URL.Hostname()); err != nil {
				return err
			}
			return nil
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: 8 * time.Second}
				return dialPublic(ctx, network, addr, net.DefaultResolver.LookupIP, dialer.DialContext)
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "image/") {
		return nil, fmt.Errorf("content-type %q is not an image", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetchBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFetchBytes {
		return nil, errors.New("remote image too large")
	}
	return s.SaveBytesContext(ctx, data, "url", clientID)
}

func checkHost(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("dns lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("no address")
	}
	for _, ip := range ips {
		if isForbiddenIP(ip) {
			return fmt.Errorf("address %s is not allowed", ip)
		}
	}
	return nil
}

// ensureFile makes sure a deduped row still has its bytes on this host.
// The database may be shared with another environment, so a media row can
// exist while the file only lives elsewhere.
func (s *Store) ensureFile(rel string, data []byte) error {
	full := filepath.Join(s.Dir, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return writeAtomic(full, data)
}

func isForbiddenIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.ParseIP("169.254.169.254"))
}

func hasAlpha(img image.Image) bool {
	b := img.Bounds()
	step := 1
	if b.Dx()*b.Dy() > 400_000 {
		step = 4
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

// Resolve once and dial only a validated literal IP. HTTP Host and TLS SNI
// continue to come from the original request URL in net/http.Transport.
func dialPublic(ctx context.Context, network, addr string, lookup func(context.Context, string, string) ([]net.IP, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("no address")
	}
	for _, ip := range ips {
		if isForbiddenIP(ip) {
			return nil, errors.New("non-public address denied")
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		if _, existing := os.Stat(path); existing == nil {
			return nil
		}
		return err
	}
	return nil
}
