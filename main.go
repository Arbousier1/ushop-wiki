package main

import (
    "encoding/json"
    "fmt"
    "html"
    "hash/fnv"
    "io"
    "log"
    "net/http"
    "net/url"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "time"
)

var (
    anchorPattern            = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))[^>]*>(.*?)</a>`)
    tagPattern               = regexp.MustCompile(`(?is)<[^>]+>`)
    titlePattern             = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
    invalidFilenameCharRegex = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

const (
    startURL      = "https://ultimateshop.superiormc.cn/"
    outputRootDir = "output/wiki"
    maxPages      = 200
)

var blockedExtensions = map[string]struct{}{
    ".css":   {},
    ".js":    {},
    ".map":   {},
    ".png":   {},
    ".jpg":   {},
    ".jpeg":  {},
    ".gif":   {},
    ".svg":   {},
    ".webp":  {},
    ".ico":   {},
    ".woff":  {},
    ".woff2": {},
    ".ttf":   {},
    ".eot":   {},
    ".xml":   {},
    ".pdf":   {},
    ".zip":   {},
    ".gz":    {},
}

type Page struct {
    URL   string   `json:"url"`
    File  string   `json:"file"`
    Title string   `json:"title,omitempty"`
    Links []string `json:"links"`
}

type CrawlIndex struct {
    StartURL  string `json:"start_url"`
    PageCount int    `json:"page_count"`
    Pages     []Page `json:"pages"`
}

func fetch(client *http.Client, rawURL string) (string, error) {
    req, err := http.NewRequest(http.MethodGet, rawURL, nil)
    if err != nil {
        return "", err
    }
    req.Header.Set("User-Agent", "ushop-wiki-scraper/1.0 (+https://github.com/example/ushop-wiki)")

    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("bad status: %s", resp.Status)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }
    return string(body), nil
}

func parseLinks(content string, base *url.URL) []string {
    matches := anchorPattern.FindAllStringSubmatch(content, -1)
    seen := make(map[string]struct{}, len(matches))
    links := make([]string, 0, len(matches))

    for _, m := range matches {
        href := m[1]
        if href == "" {
            href = m[2]
        }
        if href == "" {
            href = m[3]
        }
        href = strings.TrimSpace(html.UnescapeString(href))
        normalized, err := normalizeURL(href, base)
        if err != nil || normalized == "" {
            continue
        }
        if _, ok := seen[normalized]; ok {
            continue
        }
        seen[normalized] = struct{}{}
        links = append(links, normalized)
    }
    sort.Strings(links)
    return links
}

func normalizeURL(raw string, base *url.URL) (string, error) {
    if raw == "" {
        return "", nil
    }

    lowered := strings.ToLower(strings.TrimSpace(raw))
    if strings.HasPrefix(lowered, "#") ||
        strings.HasPrefix(lowered, "javascript:") ||
        strings.HasPrefix(lowered, "mailto:") ||
        strings.HasPrefix(lowered, "tel:") {
        return "", nil
    }

    parsed, err := base.Parse(raw)
    if err != nil {
        return "", err
    }

    if parsed.Scheme != "http" && parsed.Scheme != "https" {
        return "", nil
    }
    if !strings.EqualFold(parsed.Host, base.Host) {
        return "", nil
    }

    parsed.Fragment = ""
    parsed.RawQuery = ""
    parsed.Path = cleanURLPath(parsed.Path)

    if shouldSkipPath(parsed.Path) {
        return "", nil
    }

    return parsed.String(), nil
}

func cleanURLPath(p string) string {
    if p == "" {
        return "/"
    }
    cleaned := path.Clean(p)
    if cleaned == "." {
        return "/"
    }
    if !strings.HasPrefix(cleaned, "/") {
        cleaned = "/" + cleaned
    }
    return cleaned
}

func shouldSkipPath(p string) bool {
    lower := strings.ToLower(p)
    if strings.HasPrefix(lower, "/~gitbook/") {
        return true
    }

    ext := strings.ToLower(filepath.Ext(lower))
    if _, blocked := blockedExtensions[ext]; blocked {
        return true
    }
    return false
}

func extractTitle(content string) string {
    match := titlePattern.FindStringSubmatch(content)
    if len(match) < 2 {
        return ""
    }

    title := tagPattern.ReplaceAllString(match[1], "")
    title = strings.TrimSpace(html.UnescapeString(title))
    title = strings.Join(strings.Fields(title), " ")
    return title
}

func sanitizeFileSegment(segment string) string {
    s := strings.TrimSpace(segment)
    if s == "" {
        return "_"
    }

    s = invalidFilenameCharRegex.ReplaceAllString(s, "_")
    s = strings.Trim(s, " .")
    if s == "" {
        return "_"
    }
    return s
}

func shortHash(input string) string {
    h := fnv.New32a()
    _, _ = h.Write([]byte(input))
    return fmt.Sprintf("%08x", h.Sum32())
}

func localFilePath(rawURL string) (string, error) {
    parsed, err := url.Parse(rawURL)
    if err != nil {
        return "", err
    }

    relPath := strings.Trim(parsed.Path, "/")
    if relPath == "" {
        relPath = "index"
    } else if strings.HasSuffix(parsed.Path, "/") {
        relPath = relPath + "/index"
    }

    parts := strings.Split(relPath, "/")
    for i := range parts {
        parts[i] = sanitizeFileSegment(parts[i])
    }

    safeRelPath := filepath.Join(parts...)
    if parsed.RawQuery != "" {
        safeRelPath += "_" + shortHash(parsed.RawQuery)
    }

    return filepath.Join(outputRootDir, "pages", safeRelPath+".html"), nil
}

func writeFile(path string, content string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    return os.WriteFile(path, []byte(content), 0o644)
}

func crawlSite(start string, limit int) (CrawlIndex, error) {
    parsedStart, err := url.Parse(start)
    if err != nil {
        return CrawlIndex{}, err
    }

    client := &http.Client{
        Timeout: 30 * time.Second,
    }

    queue := []string{start}
    seen := make(map[string]struct{})
    queued := map[string]struct{}{
        start: {},
    }
    pages := make([]Page, 0, limit)

    for len(queue) > 0 && len(pages) < limit {
        current := queue[0]
        queue = queue[1:]
        delete(queued, current)

        if _, done := seen[current]; done {
            continue
        }
        seen[current] = struct{}{}

        log.Printf("Fetching %s", current)
        body, err := fetch(client, current)
        if err != nil {
            log.Printf("Skipping %s: %v", current, err)
            continue
        }

        base, err := url.Parse(current)
        if err != nil {
            return CrawlIndex{}, err
        }

        links := parseLinks(body, base)
        title := extractTitle(body)

        for _, link := range links {
            if _, done := seen[link]; done {
                continue
            }
            if _, waiting := queued[link]; waiting {
                continue
            }
            queue = append(queue, link)
            queued[link] = struct{}{}
        }

        filePath, err := localFilePath(current)
        if err != nil {
            return CrawlIndex{}, err
        }

        if err := writeFile(filePath, body); err != nil {
            return CrawlIndex{}, err
        }

        pages = append(pages, Page{
            URL:   current,
            File:  filepath.ToSlash(filePath),
            Title: title,
            Links: links,
        })
    }

    sort.Slice(pages, func(i, j int) bool {
        return pages[i].URL < pages[j].URL
    })

    if len(pages) >= limit {
        log.Printf("Crawl limit reached (%d pages)", limit)
    }

    return CrawlIndex{
        StartURL:  parsedStart.String(),
        PageCount: len(pages),
        Pages:     pages,
    }, nil
}

func writeIndexFile(index CrawlIndex) error {
    if err := os.MkdirAll(outputRootDir, 0o755); err != nil {
        return err
    }

    bytes, err := json.MarshalIndent(index, "", "  ")
    if err != nil {
        return err
    }
    bytes = append(bytes, '\n')

    return os.WriteFile(filepath.Join(outputRootDir, "index.json"), bytes, 0o644)
}

func main() {
    log.Printf("Starting crawl: %s", startURL)
    index, err := crawlSite(startURL, maxPages)
    if err != nil {
        log.Fatal(err)
    }

    if err := writeIndexFile(index); err != nil {
        log.Fatal(err)
    }

    log.Printf("Saved %d pages into %s", index.PageCount, outputRootDir)
    log.Printf("Index file: %s", filepath.ToSlash(filepath.Join(outputRootDir, "index.json")))
}
