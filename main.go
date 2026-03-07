package main

import (
    "fmt"
    "io"
    "log"
    "net/http"
    "strings"

    "github.com/PuerkitoBio/goquery"
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

func parseLinks(html string) error {
    doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
    if err != nil {
        return err
    }
    doc.Find("a").Each(func(i int, s *goquery.Selection) {
        href, _ := s.Attr("href")
        text := strings.TrimSpace(s.Text())
        fmt.Printf("link %d: text=%s href=%s\n", i, text, href)
    })
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
