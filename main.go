package main

import (
    "fmt"
    "html"
    "io"
    "log"
    "net/http"
    "regexp"
    "strings"
)

var (
    anchorPattern = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*(['"])(.*?)\1[^>]*>(.*?)</a>`)
    tagPattern    = regexp.MustCompile(`(?is)<[^>]+>`)
)

func fetch(url string) (string, error) {
    resp, err := http.Get(url)
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

func parseLinks(content string) error {
    matches := anchorPattern.FindAllStringSubmatch(content, -1)
    for i, m := range matches {
        href := strings.TrimSpace(html.UnescapeString(m[2]))
        text := tagPattern.ReplaceAllString(m[3], "")
        text = strings.Join(strings.Fields(strings.TrimSpace(html.UnescapeString(text))), " ")
        fmt.Printf("link %d: text=%s href=%s\n", i, text, href)
    }
    return nil
}

func main() {
    url := "https://ultimateshop.superiormc.cn/"
    fmt.Println("Fetching", url)
    body, err := fetch(url)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Downloaded %d bytes\n", len(body))

    if err := parseLinks(body); err != nil {
        log.Println("parse error:", err)
    }
}
