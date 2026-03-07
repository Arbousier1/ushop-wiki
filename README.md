# uShop Wiki Scraper

This repository contains a simple Go program that scrapes https://ultimateshop.superiormc.cn/ and a GitHub Action workflow to run it on a schedule.

## Getting started

1. Install [Go](https://golang.org/dl/) (1.20 or later).
2. Clone the repo.
3. Run:
   ```sh
   go mod tidy
   go run main.go
   ```

The program fetches the homepage, prints the number of bytes downloaded, and lists all anchor links found on the page.

## GitHub Actions

Workflow is defined at `.github/workflows/scrape.yml`. It triggers on push to `main`, on a daily cron schedule, and can be manually dispatched. The job builds the project and executes `main.go`.
