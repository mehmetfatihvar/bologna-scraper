package main

import (
	"os"

	"bologna-scraper/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
