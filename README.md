# uShop Wiki Scraper

This repository contains a Go crawler for https://ultimateshop.superiormc.cn/ and a GitHub Action workflow that runs it on a schedule.

## Getting started

1. Install [Go](https://golang.org/dl/) (1.20 or later).
2. Clone the repo.
3. Run:
   ```sh
   go run main.go
   ```

The crawler saves a snapshot into `output/wiki`:
- `output/wiki/index.json`: metadata and discovered page list
- `output/wiki/pages/*.html`: raw HTML content per crawled page
- `output/wiki/txt/*.txt`: plain text per crawled page (AI-friendly)
- `output/wiki/wiki.txt`: merged plain text of all crawled pages

## GitHub Actions

Workflow is defined at `.github/workflows/scrape.yml`.

It triggers on:
- push to `main` (except `output/wiki/**`)
- daily cron schedule
- manual dispatch

The job:
1. builds the project
2. runs the crawler
3. uploads `output/wiki` as a workflow artifact
4. auto-commits and pushes `output/wiki` changes back to `main`
