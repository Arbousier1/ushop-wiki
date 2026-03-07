package main

import (
    "fmt"
    "html"
    "io"
    "log"
    "net/http"
    "net/url"
    "os"
    "path"
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

type PageData struct {
    URL   string
    Title string
    Links []string
    Text  string
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

    ext := strings.ToLower(path.Ext(lower))
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

func crawlSite(start string, limit int) ([]PageData, error) {
    if _, err := url.Parse(start); err != nil {
        return nil, err
    }

    client := &http.Client{
        Timeout: 30 * time.Second,
    }

    queue := []string{start}
    seen := make(map[string]struct{})
    queued := map[string]struct{}{
        start: {},
    }
    pages := make([]PageData, 0, limit)

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
            return nil, err
        }

        links := parseLinks(body, base)
        title := extractTitle(body)
        plainText := htmlToText(body)

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

        pages = append(pages, PageData{
            URL:   current,
            Title: title,
            Links: links,
            Text:  plainText,
        })
    }

    sort.Slice(pages, func(i, j int) bool {
        return pages[i].URL < pages[j].URL
    })

    if len(pages) >= limit {
        log.Printf("Crawl limit reached (%d pages)", limit)
    }

    return pages, nil
}

func writeCombinedTXT(pages []PageData) error {
    if err := os.RemoveAll(outputRootDir); err != nil {
        return err
    }
    if err := os.MkdirAll(outputRootDir, 0o755); err != nil {
        return err
    }

    var b strings.Builder
    b.WriteString("uShop Wiki Snapshot\n")
    b.WriteString(fmt.Sprintf("Generated UTC: %s\n", time.Now().UTC().Format(time.RFC3339)))
    b.WriteString(fmt.Sprintf("Page Count: %d\n\n", len(pages)))

    for _, page := range pages {
        b.WriteString("============================================================\n")
        if page.Title != "" {
            b.WriteString("Title: ")
            b.WriteString(page.Title)
            b.WriteString("\n")
        }
        b.WriteString("URL: ")
        b.WriteString(page.URL)
        b.WriteString("\n\n")

        if len(page.Links) > 0 {
            b.WriteString(fmt.Sprintf("Links: %d\n\n", len(page.Links)))
        }

        if page.Text == "" {
            b.WriteString("[No readable text extracted]\n\n")
            continue
        }

        b.WriteString(page.Text)
        b.WriteString("\n\n")
    }

    return os.WriteFile(path.Join(outputRootDir, combinedTXT), []byte(b.String()), 0o644)
}

func main() {
    log.Printf("Starting crawl: %s", startURL)
    pages, err := crawlSite(startURL, maxPages)
    if err != nil {
        log.Fatal(err)
    }

    if err := writeCombinedTXT(pages); err != nil {
        log.Fatal(err)
    }

    log.Printf("Saved 1 text file into %s", outputRootDir)
    log.Printf("TXT file: %s", path.Join(outputRootDir, combinedTXT))
}
