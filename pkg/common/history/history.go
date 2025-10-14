// Package history provides functions to save and load history in a streamlined way.
package history

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

type HistoryData struct {
	LastUsed time.Time
	Amount   int
}

const ActionDelete = "erase_history"

// TODO: this is global for every history ... should not be the case. Just a crutch because of gob encoding.
var mut sync.Mutex

type History struct {
	Provider string
	Data     map[string]map[string]*HistoryData
}

func (h *History) Remove(identifier string) {
	mut.Lock()
	defer mut.Unlock()

	for _, v := range h.Data {
		delete(v, identifier)
	}

	h.writeFile()
}

func (h *History) Save(query, identifier string) {
	mut.Lock()
	defer mut.Unlock()

	if _, ok := h.Data[query]; ok {
		if val, ok := h.Data[query][identifier]; ok {
			h.Data[query][identifier].LastUsed = time.Now()
			h.Data[query][identifier].Amount = min(val.Amount+1, 10)
		} else {
			h.Data[query][identifier] = &HistoryData{
				LastUsed: time.Now(),
				Amount:   1,
			}
		}
	} else {
		h.Data[query] = make(map[string]*HistoryData)
		h.Data[query][identifier] = &HistoryData{
			LastUsed: time.Now(),
			Amount:   1,
		}
	}

	h.writeFile()
}

func (h *History) writeFile() {
	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(h)
	if err != nil {
		slog.Error("history", "encode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(common.CacheFile(fmt.Sprintf("%s_history.gob", h.Provider))), 0o755)
	if err != nil {
		slog.Error("history", "createdirs", err)
		return
	}

	err = os.WriteFile(common.CacheFile(fmt.Sprintf("%s_history.gob", h.Provider)), b.Bytes(), 0o600)
	if err != nil {
		slog.Error("history", "writefile", err)
	}
}

func (h *History) FindUsage(query, identifier string) (int, time.Time, int) {
	mut.Lock()
	defer mut.Unlock()

	var usage int
	var lastUsed time.Time

	if query == "" {
		for _, v := range h.Data {
			if n, ok := v[identifier]; ok {
				usage += n.Amount
				if n.LastUsed.After(lastUsed) {
					lastUsed = n.LastUsed
				}
			}
		}
		return usage, lastUsed, 0
	}

	delta := 0

	for k, v := range h.Data {
		if strings.HasPrefix(query, k) || strings.HasPrefix(k, query) {
			delta = len(k) - len(query)

			if delta < 0 {
				delta = delta * -1
			}

			if n, ok := v[identifier]; ok {
				usage += n.Amount
				if n.LastUsed.After(lastUsed) {
					lastUsed = n.LastUsed
				}
			}
		}
	}

	return usage, lastUsed, delta
}

func (h *History) CalcUsageScore(query, identifier string) int32 {
	amount, last, delta := h.FindUsage(query, identifier)

	if amount == 0 {
		return 0
	}

	base := 10

	if amount > 0 {
		today := time.Now()
		duration := today.Sub(last)
		days := int(duration.Hours() / 24)

		if days > 0 {
			base -= days
		}

		res := max(base*amount, 1)

		if delta != 0 {
			return int32(res / delta)
		}

		return int32(res)
	}

	return 0
}

func Load(provider string) *History {
	h := History{
		Data:     make(map[string]map[string]*HistoryData),
		Provider: provider,
	}

	file := common.CacheFile(fmt.Sprintf("%s_history.gob", provider))

	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error("history", "load", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&h)
			if err != nil {
				slog.Error("history", "decoding", err)
			}
		}
	}

	return &h
}
