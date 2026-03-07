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
    bodyPattern              = regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
    dropContentPattern       = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>|<template[^>]*>.*?</template>|<svg[^>]*>.*?</svg>`)
    breakTagPattern          = regexp.MustCompile(`(?is)<br\s*/?>|</?(?:p|div|section|article|main|header|footer|aside|nav|h[1-6]|li|ul|ol|table|tr|td|th|blockquote|pre)[^>]*>`)
    spacePattern             = regexp.MustCompile(`[ \t]+`)
    invalidFilenameCharRegex = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

const (
    startURL      = "https://ultimateshop.superiormc.cn/"
    outputRootDir = "output/wiki"
    maxPages      = 200
    combinedTXT   = "wiki.txt"
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
    TextFile string   `json:"text_file,omitempty"`
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

func localPathPrefix(rawURL string) (string, error) {
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

    return safeRelPath, nil
}

func localHTMLFilePath(rawURL string) (string, error) {
    relPrefix, err := localPathPrefix(rawURL)
    if err != nil {
        return "", err
    }
    return filepath.Join(outputRootDir, "pages", relPrefix+".html"), nil
}

func localTXTFilePath(rawURL string) (string, error) {
    relPrefix, err := localPathPrefix(rawURL)
    if err != nil {
        return "", err
    }
    return filepath.Join(outputRootDir, "txt", relPrefix+".txt"), nil
}

func writeFile(path string, content string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    return os.WriteFile(path, []byte(content), 0o644)
}

func htmlToText(content string) string {
    body := content
    if match := bodyPattern.FindStringSubmatch(content); len(match) > 1 {
        body = match[1]
    }

    body = dropContentPattern.ReplaceAllString(body, "")
    body = breakTagPattern.ReplaceAllString(body, "\n")
    body = tagPattern.ReplaceAllString(body, "")
    body = html.UnescapeString(body)

    lines := strings.Split(body, "\n")
    cleaned := make([]string, 0, len(lines))
    previousBlank := false

    for _, line := range lines {
        line = strings.TrimSpace(spacePattern.ReplaceAllString(line, " "))
        if line == "" {
            if !previousBlank && len(cleaned) > 0 {
                cleaned = append(cleaned, "")
            }
            previousBlank = true
            continue
        }

        cleaned = append(cleaned, line)
        previousBlank = false
    }

    for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
        cleaned = cleaned[:len(cleaned)-1]
    }

    return strings.Join(cleaned, "\n")
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

        htmlPath, err := localHTMLFilePath(current)
        if err != nil {
            return CrawlIndex{}, err
        }

        if err := writeFile(htmlPath, body); err != nil {
            return CrawlIndex{}, err
        }

        txtPath, err := localTXTFilePath(current)
        if err != nil {
            return CrawlIndex{}, err
        }

        plainText := htmlToText(body)
        if err := writeFile(txtPath, plainText+"\n"); err != nil {
            return CrawlIndex{}, err
        }

        pages = append(pages, Page{
            URL:   current,
            File:  filepath.ToSlash(htmlPath),
            TextFile: filepath.ToSlash(txtPath),
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

func writeCombinedTXT(index CrawlIndex) error {
    if err := os.MkdirAll(outputRootDir, 0o755); err != nil {
        return err
    }

    var b strings.Builder
    b.WriteString("uShop Wiki Snapshot\n")
    b.WriteString(fmt.Sprintf("Generated UTC: %s\n", time.Now().UTC().Format(time.RFC3339)))
    b.WriteString(fmt.Sprintf("Page Count: %d\n\n", index.PageCount))

    for _, page := range index.Pages {
        b.WriteString("============================================================\n")
        if page.Title != "" {
            b.WriteString("Title: ")
            b.WriteString(page.Title)
            b.WriteString("\n")
        }
        b.WriteString("URL: ")
        b.WriteString(page.URL)
        b.WriteString("\n\n")

        if page.TextFile == "" {
            b.WriteString("[No text output]\n\n")
            continue
        }

        txtPath := filepath.FromSlash(page.TextFile)
        body, err := os.ReadFile(txtPath)
        if err != nil {
            return err
        }

        b.Write(body)
        b.WriteString("\n\n")
    }

    return os.WriteFile(filepath.Join(outputRootDir, combinedTXT), []byte(b.String()), 0o644)
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
    if err := writeCombinedTXT(index); err != nil {
        log.Fatal(err)
    }

    log.Printf("Saved %d pages into %s", index.PageCount, outputRootDir)
    log.Printf("Index file: %s", filepath.ToSlash(filepath.Join(outputRootDir, "index.json")))
    log.Printf("Combined TXT: %s", filepath.ToSlash(filepath.Join(outputRootDir, combinedTXT)))
}
