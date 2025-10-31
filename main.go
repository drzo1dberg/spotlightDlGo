package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Minimal Windows Spotlight downloader
// author: 2025 github.com/zo1dberg
// Portions based on ORelio/Spotlight-Downloader (MIT License)

// description :
// A minimal Windows Spotlight wallpaper downloader rewritten in Go.
const (
	userAgent = "spotlightdl-go/1.0"
)

func buildAPIURL(country, locale string) (string, error) {
	u, err := url.Parse("https://fd.api.iris.microsoft.com/v4/api/selection")
	if err != nil {
		return "", nil
	}
	q := url.Values{
		"placement": {"88000820"},
		"bcnt":      {"4"},
		"country":   {country},
		"locale":    {locale},
		"fmt":       {"json"},
	}
	u.RawQuery = q.Encode()
	return u.String(), nil

}

type (
	root struct {
		BatchRsp struct {
			Items []struct {
				Item string `json:"item"` // nested JSON string
			} `json:"items"`
		} `json:"batchrsp"`
	}

	adEnvelope struct {
		Ad *ad `json:"ad"`
	}

	ad struct {
		IconHoverText string       `json:"iconHoverText"`
		Title         string       `json:"title"`
		Copyright     string       `json:"copyright"`
		Landscape     *imageObject `json:"landscapeImage"`
	}

	imageObject struct {
		Asset string `json:"asset"`
	}

	spotImage struct {
		URL       string
		FileName  string
		Title     string
		Copyright string
	}
)

func fetchOnce(client *http.Client, country, locale string) ([]spotImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	reqURL, err := buildAPIURL(country, locale)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	var r root
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}

	var out []spotImage
	for _, it := range r.BatchRsp.Items {
		// each item is JSON inside a string
		var env adEnvelope
		if err := json.Unmarshal([]byte(it.Item), &env); err != nil {
			continue
		}
		if env.Ad == nil || env.Ad.Landscape == nil {
			continue
		}
		asset := strings.TrimSpace(env.Ad.Landscape.Asset)
		if asset == "" || !strings.HasPrefix(asset, "https://") {
			continue
		}
		out = append(out, spotImage{
			URL:       asset,
			FileName:  fileNameFromURL(asset),
			Title:     firstNonEmpty(env.Ad.IconHoverText, env.Ad.Title),
			Copyright: env.Ad.Copyright,
		})
	}
	return dedupe(out), nil
}

func dedupe(in []spotImage) []spotImage {
	seen := make(map[string]struct{})
	var out []spotImage
	for _, im := range in {
		if im.URL == "" {
			continue
		}
		if _, ok := seen[im.URL]; ok {
			continue
		}
		seen[im.URL] = struct{}{}
		out = append(out, im)
	}
	return out
}

func download(client *http.Client, src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	var expected *int64
	if resp.ContentLength > 0 {
		expected = &resp.ContentLength
	}

	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	cerr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if cerr != nil {
		os.Remove(tmp)
		return cerr
	}

	if expected != nil {
		fi, err := os.Stat(tmp)
		if err != nil {
			os.Remove(tmp)
			return err
		}
		if fi.Size() != *expected {
			os.Remove(tmp)
			return errors.New("size mismatch")
		}
	}
	return os.Rename(tmp, dst)
}

func resolveLocale(spec string) (locale, country string) {
	if spec != "" {
		parts := strings.Split(spec, "-")
		if len(parts) == 2 {
			return spec, strings.ToUpper(parts[1])
		}
	}
	lang := os.Getenv("LANG") // e.g. en_US.UTF-8
	if lang == "" {
		return "en-US", "US"
	}
	lang = strings.SplitN(lang, ".", 2)[0]
	lang = strings.ReplaceAll(lang, "_", "-") // en-US
	parts := strings.Split(lang, "-")
	if len(parts) == 2 {
		return lang, strings.ToUpper(parts[1])
	}
	return lang, "US"
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func fileNameFromURL(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	base := filepath.Base(pu.Path)
	if i := strings.IndexByte(base, '?'); i >= 0 {
		base = base[:i]
	}
	if !strings.Contains(base, ".") {
		base += ".jpg"
	}
	return base
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
func main() {
	outDir := flag.String("outdir", ".", "output directory")
	localeFlag := flag.String("locale", "", "locale like en-US (defaults from $LANG)")
	verbose := flag.Bool("v", false, "verbose logging")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	locale, country := resolveLocale(*localeFlag)
	client := &http.Client{Timeout: 20 * time.Second}

	seen := make(map[string]struct{})
	emptyRounds := 0
	const maxEmptyRounds = 50
	var totalNew int

	for emptyRounds < maxEmptyRounds {
		imgs, err := fetchOnce(client, country, locale)
		if err != nil {
			fatal(err)
		}

		newInRound := 0
		for _, im := range imgs {
			if _, ok := seen[im.URL]; ok {
				continue
			}
			seen[im.URL] = struct{}{}

			name := fileNameFromURL(im.URL)
			if name == "" {
				continue
			}
			path := filepath.Join(*outDir, name)
			if exists(path) {
				if *verbose {
					fmt.Printf("skip existing: %s\n", path)
				}
				continue
			}
			if err := download(client, im.URL, path); err != nil {
				if *verbose {
					fmt.Printf("download failed: %s: %v\n", im.URL, err)
				}
				continue
			}
			fmt.Println(path)
			newInRound++
			totalNew++
		}

		if newInRound == 0 {
			emptyRounds++
			time.Sleep(500 * time.Millisecond)
		} else {
			emptyRounds = 0
		}
	}

	if *verbose {
		fmt.Printf("done. new=%d\n", totalNew)
	}
}
