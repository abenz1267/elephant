package main

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"time"
)

type OpItem struct {
	ID                    string `json:"id"`
	Title                 string `json:"title"`
	Category              string `json:"category"`
	AdditionalInformation string `json:"additional_information"`
	Urls                  []struct {
		Href string `json:"href"`
	} `json:"urls"`
}

func checkAvailable() {
	for {
		cmd := exec.Command("op", "item", "list")

		err := cmd.Run()
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		return
	}
}

func initItems() {
	checkAvailable()

	cachedItems = []OpItem{}

	for _, v := range config.Vaults {
		cmd := exec.Command("op", "item", "list", "--format=json", "--vault", v)

		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "init", err, "msg", output)
			continue
		}

		var items []OpItem

		if err := json.Unmarshal(output, &items); err != nil {
			slog.Error(Name, "parse", err, "msg", output)
			continue
		}

		cachedItems = append(cachedItems, items...)
	}
}
